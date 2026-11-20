package etcd

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/magalab/dbl/internal/connection"
	serverpb "go.etcd.io/etcd/api/v3/etcdserverpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

const maxMetricsBytes = 4 << 20

func newMetricsHTTPClient(tlsConfig *tls.Config) *http.Client {
	transport := &http.Transport{TLSClientConfig: tlsConfig}
	return &http.Client{Transport: transport, Timeout: 4 * time.Second}
}

func (s *Store) Dashboard(ctx context.Context) (connection.EtcdDashboard, error) {
	if err := s.ensureOpen(); err != nil {
		return connection.EtcdDashboard{}, err
	}
	dashboard := connection.EtcdDashboard{}
	memberList, err := s.client.MemberList(ctx)
	if err != nil {
		dashboard.MemberListErr = err.Error()
	} else {
		for _, member := range memberList.Members {
			if member == nil {
				continue
			}
			dashboard.Members = append(dashboard.Members, connection.EtcdMember{
				ID: member.ID, Name: member.Name, ClientURLs: append([]string(nil), member.ClientURLs...),
				PeerURLs: append([]string(nil), member.PeerURLs...), IsLearner: member.IsLearner,
			})
		}
	}

	alarms, err := s.client.AlarmList(ctx)
	if err != nil {
		dashboard.AlarmListErr = err.Error()
	} else {
		for _, alarm := range alarms.Alarms {
			if alarm != nil {
				dashboard.Alarms = append(dashboard.Alarms, connection.EtcdAlarm{MemberID: alarm.MemberID, Type: alarm.Alarm.String()})
			}
		}
	}

	auth, err := s.client.AuthStatus(ctx)
	if err != nil {
		dashboard.AuthStatusErr = err.Error()
	} else {
		dashboard.AuthEnabled, dashboard.AuthRevision = auth.Enabled, auth.AuthRevision
	}

	s.probeDashboardMembers(ctx, &dashboard)
	endpoints := s.client.Endpoints()
	if len(endpoints) > 0 {
		dashboard.Metrics = s.scrapeMetrics(ctx, endpoints[0])
	}
	for _, member := range dashboard.Members {
		if member.Reachable {
			dashboard.Revision = member.Revision
			break
		}
	}
	return dashboard, nil
}

type memberProbe struct {
	memberID uint64
	endpoint string
}

type memberProbeResult struct {
	memberID uint64
	endpoint string
	status   *clientv3.StatusResponse
	err      error
}

func (s *Store) probeDashboardMembers(ctx context.Context, dashboard *connection.EtcdDashboard) {
	probes := make([]memberProbe, 0, len(dashboard.Members)+len(s.client.Endpoints()))
	seen := make(map[string]bool)
	for _, endpoint := range s.client.Endpoints() {
		if !seen[endpoint] {
			probes = append(probes, memberProbe{endpoint: endpoint})
			seen[endpoint] = true
		}
	}
	for _, member := range dashboard.Members {
		for _, endpoint := range member.ClientURLs {
			if endpoint != "" && !seen[endpoint] {
				probes = append(probes, memberProbe{memberID: member.ID, endpoint: endpoint})
				seen[endpoint] = true
			}
		}
	}
	if len(probes) > 64 {
		probes = probes[:64]
	}
	results := make(chan memberProbeResult, len(probes))
	var workers sync.WaitGroup
	for _, probe := range probes {
		probe := probe
		workers.Add(1)
		go func() {
			defer workers.Done()
			probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			status, err := s.client.Status(probeCtx, probe.endpoint)
			results <- memberProbeResult{memberID: probe.memberID, endpoint: probe.endpoint, status: status, err: err}
		}()
	}
	workers.Wait()
	close(results)
	for result := range results {
		memberID := result.memberID
		if result.err == nil {
			if result.status == nil || result.status.Header == nil {
				result.err = errors.New("etcd returned an incomplete status response")
			} else {
				memberID = result.status.Header.GetMemberId()
			}
		}
		memberIndex := -1
		for i := range dashboard.Members {
			member := &dashboard.Members[i]
			if (memberID != 0 && member.ID == memberID) || member.Endpoint == result.endpoint {
				memberIndex = i
				break
			}
			if result.err != nil && memberID == 0 {
				for _, endpoint := range member.ClientURLs {
					if endpoint == result.endpoint {
						memberIndex = i
						break
					}
				}
				if memberIndex >= 0 {
					break
				}
			}
		}
		if memberIndex < 0 {
			if dashboard.MemberListErr == "" {
				continue
			}
			dashboard.Members = append(dashboard.Members, connection.EtcdMember{ID: memberID, Endpoint: result.endpoint})
			memberIndex = len(dashboard.Members) - 1
		}
		member := &dashboard.Members[memberIndex]
		if result.err != nil {
			if !member.Reachable && member.StatusError == "" {
				member.StatusError = result.err.Error()
			}
			continue
		}
		member.Reachable = true
		member.Endpoint = result.endpoint
		member.StatusError = ""
		member.Version = result.status.Version
		member.ReportedMemberID = result.status.Header.GetMemberId()
		member.LeaderID = result.status.Leader
		member.Revision = result.status.Header.GetRevision()
		member.RaftIndex = result.status.RaftIndex
		member.RaftAppliedIndex = result.status.RaftAppliedIndex
		member.RaftTerm = result.status.Header.GetRaftTerm()
		member.BackendSize = result.status.DbSize
		member.BackendSizeInUse = result.status.DbSizeInUse
		member.BackendSizeQuota = result.status.DbSizeQuota
		member.ServerErrors = append([]string(nil), result.status.Errors...)
		if dashboard.ClusterID == 0 {
			dashboard.ClusterID = result.status.Header.GetClusterId()
		}
	}
}

func (s *Store) scrapeMetrics(ctx context.Context, endpoint string) connection.EtcdMetrics {
	client := s.metricsClient
	if client == nil {
		client = newMetricsHTTPClient(nil)
	}
	if !strings.Contains(endpoint, "://") {
		scheme := "http://"
		if transport, ok := client.Transport.(*http.Transport); ok && transport.TLSClientConfig != nil {
			scheme = "https://"
		}
		endpoint = scheme + endpoint
	}
	metrics := connection.EtcdMetrics{Endpoint: strings.TrimRight(endpoint, "/") + "/metrics", SampledAt: time.Now()}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, metrics.Endpoint, nil)
	if err != nil {
		metrics.Error = err.Error()
		return metrics
	}
	request.Header.Set("Accept", "text/plain; version=0.0.4")
	response, err := client.Do(request)
	if err != nil {
		metrics.Error = err.Error()
		return metrics
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		metrics.Error = fmt.Sprintf("metrics endpoint returned HTTP %d", response.StatusCode)
		return metrics
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxMetricsBytes+1))
	if err != nil {
		metrics.Error = err.Error()
		return metrics
	}
	if len(body) > maxMetricsBytes {
		metrics.Error = "metrics response exceeds the 4 MiB read limit"
		return metrics
	}
	values := map[string]float64{}
	grpcFailures := float64(0)
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	scanner.Buffer(make([]byte, 16*1024), maxMetricsBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		if brace := strings.IndexByte(name, '{'); brace >= 0 {
			name = name[:brace]
		}
		value, parseErr := strconv.ParseFloat(fields[1], 64)
		if parseErr != nil {
			continue
		}
		values[name] += value
		if name == "grpc_server_handled_total" && !strings.Contains(fields[0], `grpc_code="OK"`) {
			grpcFailures += value
		}
	}
	if err := scanner.Err(); err != nil {
		metrics.Error = err.Error()
		return metrics
	}
	metrics.Available = true
	metrics.HasLeader = values["etcd_server_has_leader"]
	metrics.LeaderChanges = values["etcd_server_leader_changes_seen_total"]
	metrics.ProposalsCommitted = values["etcd_server_proposals_committed_total"]
	metrics.ProposalsApplied = values["etcd_server_proposals_applied_total"]
	metrics.ProposalsPending = values["etcd_server_proposals_pending"]
	metrics.ProposalsFailed = values["etcd_server_proposals_failed_total"]
	metrics.BackendSize = values["etcd_mvcc_db_total_size_in_bytes"]
	metrics.BackendSizeInUse = values["etcd_mvcc_db_total_size_in_use_in_bytes"]
	metrics.BackendQuota = values["etcd_server_quota_backend_bytes"]
	metrics.ResidentMemoryBytes = values["process_resident_memory_bytes"]
	metrics.Goroutines = values["go_goroutines"]
	metrics.GRPCRequestsTotal = values["grpc_server_started_total"]
	metrics.GRPCFailuresTotal = grpcFailures
	if started := values["process_start_time_seconds"]; started > 0 {
		metrics.UptimeSeconds = max(0, float64(time.Now().Unix())-started)
	}
	return metrics
}

func (s *Store) AuthOverview(ctx context.Context) (connection.EtcdAuthOverview, error) {
	if err := s.ensureOpen(); err != nil {
		return connection.EtcdAuthOverview{}, err
	}
	overview := connection.EtcdAuthOverview{}
	if status, err := s.client.AuthStatus(ctx); err != nil {
		overview.StatusError = err.Error()
	} else {
		overview.Enabled, overview.Revision = status.Enabled, status.AuthRevision
	}
	if users, err := s.client.UserList(ctx); err != nil {
		overview.UsersError = err.Error()
	} else {
		overview.Users = append([]string(nil), users.Users...)
	}
	if roles, err := s.client.RoleList(ctx); err != nil {
		overview.RolesError = err.Error()
	} else {
		overview.Roles = append([]string(nil), roles.Roles...)
	}
	return overview, nil
}

func (s *Store) UserDetails(ctx context.Context, name string) (connection.EtcdUser, error) {
	if err := s.ensureOpen(); err != nil {
		return connection.EtcdUser{}, err
	}
	response, err := s.client.UserGet(ctx, strings.TrimSpace(name))
	if err != nil {
		return connection.EtcdUser{}, err
	}
	return connection.EtcdUser{Name: strings.TrimSpace(name), Roles: append([]string(nil), response.Roles...)}, nil
}

func (s *Store) RoleDetails(ctx context.Context, name string) (connection.EtcdRole, error) {
	if err := s.ensureOpen(); err != nil {
		return connection.EtcdRole{}, err
	}
	response, err := s.client.RoleGet(ctx, strings.TrimSpace(name))
	if err != nil {
		return connection.EtcdRole{}, err
	}
	role := connection.EtcdRole{Name: strings.TrimSpace(name)}
	for _, permission := range response.Perm {
		if permission != nil {
			role.Permissions = append(role.Permissions, connection.EtcdPermission{
				Type: permission.PermType.String(), Key: string(permission.Key), RangeEnd: string(permission.RangeEnd),
			})
		}
	}
	return role, nil
}

func (s *Store) SetAuthEnabled(ctx context.Context, enabled bool) error {
	if err := s.checkWritable(); err != nil {
		return err
	}
	if enabled {
		_, err := s.client.AuthEnable(ctx)
		return err
	}
	_, err := s.client.AuthDisable(ctx)
	return err
}

func (s *Store) CreateUser(ctx context.Context, name, password string) error {
	if err := s.checkWritable(); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" || password == "" {
		return errors.New("user name and password are required")
	}
	_, err := s.client.UserAdd(ctx, name, password)
	return err
}

func (s *Store) DeleteUser(ctx context.Context, name string) error {
	if err := s.checkWritable(); err != nil {
		return err
	}
	if name = strings.TrimSpace(name); name == "" {
		return errors.New("user name is required")
	}
	_, err := s.client.UserDelete(ctx, name)
	return err
}

func (s *Store) ChangeUserPassword(ctx context.Context, name, password string) error {
	if err := s.checkWritable(); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" || password == "" {
		return errors.New("user name and password are required")
	}
	_, err := s.client.UserChangePassword(ctx, name, password)
	return err
}

func (s *Store) GrantUserRole(ctx context.Context, name, role string) error {
	if err := s.checkWritable(); err != nil {
		return err
	}
	name, role = strings.TrimSpace(name), strings.TrimSpace(role)
	if name == "" || role == "" {
		return errors.New("user and role are required")
	}
	_, err := s.client.UserGrantRole(ctx, name, role)
	return err
}

func (s *Store) RevokeUserRole(ctx context.Context, name, role string) error {
	if err := s.checkWritable(); err != nil {
		return err
	}
	name, role = strings.TrimSpace(name), strings.TrimSpace(role)
	if name == "" || role == "" {
		return errors.New("user and role are required")
	}
	_, err := s.client.UserRevokeRole(ctx, name, role)
	return err
}

func (s *Store) CreateRole(ctx context.Context, name string) error {
	if err := s.checkWritable(); err != nil {
		return err
	}
	if name = strings.TrimSpace(name); name == "" {
		return errors.New("role name is required")
	}
	_, err := s.client.RoleAdd(ctx, name)
	return err
}

func (s *Store) DeleteRole(ctx context.Context, name string) error {
	if err := s.checkWritable(); err != nil {
		return err
	}
	if name = strings.TrimSpace(name); name == "" {
		return errors.New("role name is required")
	}
	_, err := s.client.RoleDelete(ctx, name)
	return err
}

func (s *Store) GrantRolePermission(ctx context.Context, role, key, rangeEnd, permission string) error {
	if err := s.checkWritable(); err != nil {
		return err
	}
	role, permission = strings.TrimSpace(role), strings.ToUpper(strings.TrimSpace(permission))
	if role == "" || (key == "" && rangeEnd != "\x00") || permission == "" {
		return errors.New("role, key and permission are required")
	}
	if rangeEnd != "" && rangeEnd != "\x00" && rangeEnd <= key {
		return errors.New("permission range end must be greater than its start key")
	}
	permType, err := clientv3.StrToPermissionType(permission)
	if err != nil {
		return err
	}
	_, err = s.client.RoleGrantPermission(ctx, role, key, rangeEnd, permType)
	return err
}

func (s *Store) RevokeRolePermission(ctx context.Context, role, key, rangeEnd string) error {
	if err := s.checkWritable(); err != nil {
		return err
	}
	role = strings.TrimSpace(role)
	if role == "" || (key == "" && rangeEnd != "\x00") {
		return errors.New("role and permission key are required")
	}
	_, err := s.client.RoleRevokePermission(ctx, role, key, rangeEnd)
	return err
}

func (s *Store) Defragment(ctx context.Context, endpoint string) error {
	if err := s.checkWritable(); err != nil {
		return err
	}
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return errors.New("etcd endpoint is required")
	}
	_, err := s.client.Defragment(ctx, endpoint)
	return err
}

func (s *Store) DisarmAlarm(ctx context.Context, memberID uint64, alarm string) error {
	if err := s.checkWritable(); err != nil {
		return err
	}
	value, ok := serverpb.AlarmType_value[strings.ToUpper(strings.TrimSpace(alarm))]
	if !ok || value == int32(serverpb.AlarmType_NONE) {
		return errors.New("unknown etcd alarm type")
	}
	_, err := s.client.AlarmDisarm(ctx, &clientv3.AlarmMember{MemberID: memberID, Alarm: serverpb.AlarmType(value)})
	return err
}

var _ connection.EtcdAdmin = (*Store)(nil)
