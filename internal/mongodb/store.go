// Package mongodb adapts MongoDB to DBL's bounded Extended JSON contract.
package mongodb

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/magalab/dbl/internal/connection"
	"github.com/magalab/dbl/internal/credentials"
	"github.com/magalab/dbl/internal/preferences"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

const (
	defaultPort      = 27017
	maxPageSize      = 100
	maxDocumentBytes = 1 << 20
	maxPreviewBytes  = 4 << 10
)

type Connector struct {
	Profile preferences.ConnectionProfile
	Vault   *credentials.Vault
}

func (connector Connector) Connect(ctx context.Context) (connection.Connection, error) {
	p := connector.Profile
	if p.Driver != preferences.DriverMongoDB {
		return nil, errors.New("MongoDB connector received a different database type")
	}
	host := strings.Trim(strings.TrimSpace(p.Host), "[]")
	if host == "" {
		return nil, errors.New("MongoDB host is required")
	}
	port := p.Port
	if port == 0 {
		port = defaultPort
	}
	if port < 1 || port > 65535 {
		return nil, errors.New("MongoDB port must be between 1 and 65535")
	}
	var password []byte
	if p.CredentialRef != "" {
		secret, err := connector.Vault.Load(credentials.Reference(p.CredentialRef))
		if err != nil {
			return nil, fmt.Errorf("load MongoDB credential: %w", err)
		}
		password = secret
	}
	defer clear(password)
	clientOptions := options.Client().ApplyURI("mongodb://" + net.JoinHostPort(host, strconv.Itoa(port)))
	clientOptions.SetServerSelectionTimeout(10 * time.Second)
	clientOptions.SetConnectTimeout(10 * time.Second)
	clientOptions.SetAppName("DBL")
	if p.Username != "" || len(password) > 0 {
		clientOptions.SetAuth(options.Credential{Username: p.Username, Password: string(password), AuthSource: p.AuthSource})
	}
	tlsMode := p.TLSMode
	if tlsMode == "" {
		tlsMode = "verify-full"
	}
	if tlsMode != "disable" {
		config, err := tlsConfig(host, tlsMode, p.TLSCAPath)
		if err != nil {
			return nil, err
		}
		clientOptions.SetTLSConfig(config)
	}
	client, err := mongo.Connect(clientOptions)
	if err != nil {
		return nil, sanitizeError("configure MongoDB client", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, readpref.Primary()); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, sanitizeError("connect to MongoDB", err)
	}
	return &Store{client: client, readOnly: p.ReadOnly}, nil
}

func tlsConfig(host, mode, caPath string) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}
	if caPath != "" && mode != "verify-full" && mode != "verify-ca" {
		return nil, errors.New("MongoDB custom CA requires verify-full or verify-ca TLS mode")
	}
	switch mode {
	case "verify-full":
		if caPath != "" {
			roots, err := mongoCertPool(caPath)
			if err != nil {
				return nil, err
			}
			cfg.RootCAs = roots
		}
		return cfg, nil
	case "verify-ca":
		roots, err := mongoCertPool(caPath)
		if err != nil {
			return nil, err
		}
		cfg.RootCAs = roots
		cfg.InsecureSkipVerify = true
		cfg.VerifyConnection = func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return errors.New("MongoDB server sent no certificate")
			}
			intermediates := x509.NewCertPool()
			for _, cert := range state.PeerCertificates[1:] {
				intermediates.AddCert(cert)
			}
			_, err := state.PeerCertificates[0].Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
			return err
		}
		return cfg, nil
	case "require":
		cfg.InsecureSkipVerify = true
		return cfg, nil
	case "disable":
		return nil, nil
	default:
		return nil, errors.New("unsupported MongoDB TLS mode")
	}
}

func mongoCertPool(caPath string) (*x509.CertPool, error) {
	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		roots = x509.NewCertPool()
	}
	if caPath == "" {
		return roots, nil
	}
	pem, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("read MongoDB CA certificate: %w", err)
	}
	if !roots.AppendCertsFromPEM(pem) {
		return nil, errors.New("MongoDB CA file contains no valid certificates")
	}
	return roots, nil
}

type Store struct {
	client   *mongo.Client
	readOnly bool
}

func (s *Store) Driver() connection.Driver { return connection.DriverMongoDB }
func (s *Store) HealthCheck(ctx context.Context) error {
	if s == nil || s.client == nil {
		return errors.New("MongoDB connection is closed")
	}
	return s.client.Ping(ctx, readpref.Primary())
}
func (s *Store) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return s.client.Disconnect(ctx)
}
func (s *Store) ClassifyError(err error) connection.ErrorKind {
	if err == nil {
		return connection.ErrorUnknown
	}
	if kind := connection.ClassifyCommonError(err); kind != connection.ErrorUnknown {
		return kind
	}
	if errors.Is(err, mongo.ErrNoDocuments) {
		return connection.ErrorNotFound
	}
	var command mongo.CommandError
	if errors.As(err, &command) {
		if command.Code == 13 || command.Code == 18 {
			return connection.ErrorPermission
		}
		if command.Code == 50 {
			return connection.ErrorTimeout
		}
		return connection.ErrorQuery
	}
	if mongo.IsTimeout(err) {
		return connection.ErrorTimeout
	}
	return connection.ErrorUnknown
}

func (s *Store) Databases(ctx context.Context) ([]string, error) {
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}
	return s.client.ListDatabaseNames(ctx, bson.D{})
}
func (s *Store) Collections(ctx context.Context, database string) ([]string, error) {
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}
	return s.client.Database(database).ListCollectionNames(ctx, bson.D{})
}
func (s *Store) CollectionIndexes(ctx context.Context, database, collection string) ([]connection.DocumentIndex, error) {
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}
	cursor, err := s.client.Database(database).Collection(collection).Indexes().List(ctx)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	indexes := make([]connection.DocumentIndex, 0, 8)
	for cursor.Next(ctx) {
		var index struct {
			Name               string `bson:"name"`
			Key                bson.D `bson:"key"`
			Unique             bool   `bson:"unique"`
			Sparse             bool   `bson:"sparse"`
			ExpireAfterSeconds *int64 `bson:"expireAfterSeconds"`
		}
		if err := cursor.Decode(&index); err != nil {
			return nil, err
		}
		keys, err := bson.MarshalExtJSON(index.Key, false, false)
		if err != nil {
			return nil, fmt.Errorf("encode MongoDB index keys: %w", err)
		}
		item := connection.DocumentIndex{Name: index.Name, KeysJSON: string(keys), Unique: index.Unique, Sparse: index.Sparse}
		if index.ExpireAfterSeconds != nil {
			item.HasTTL, item.TTLSecond = true, *index.ExpireAfterSeconds
		}
		indexes = append(indexes, item)
		if len(indexes) >= 500 {
			return indexes, errors.New("MongoDB collection has more than 500 indexes; listing stopped")
		}
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}
	return indexes, nil
}
func (s *Store) FindDocuments(ctx context.Context, database, collection, filterJSON, projectionJSON string, offset uint64, limit int) (connection.DocumentPage, error) {
	if err := s.ensureOpen(); err != nil {
		return connection.DocumentPage{}, err
	}
	filter, err := decodeObject(filterJSON)
	if err != nil {
		return connection.DocumentPage{}, fmt.Errorf("invalid MongoDB filter: %w", err)
	}
	var projection any
	if strings.TrimSpace(projectionJSON) != "" {
		projection, err = decodeObject(projectionJSON)
		if err != nil {
			return connection.DocumentPage{}, fmt.Errorf("invalid MongoDB projection: %w", err)
		}
		if excludedID(projection.(bson.D)) {
			return connection.DocumentPage{}, errors.New("MongoDB projection must retain _id so documents can be opened")
		}
	}
	if offset > math.MaxInt64 {
		return connection.DocumentPage{}, errors.New("MongoDB result offset is too large")
	}
	limit = bounded(limit)
	findOptions := options.Find().SetProjection(projection).SetSort(bson.D{{Key: "_id", Value: 1}}).SetSkip(int64(offset)).SetLimit(int64(limit)).SetBatchSize(int32(limit))
	cursor, err := s.client.Database(database).Collection(collection).Find(ctx, filter, findOptions)
	if err != nil {
		return connection.DocumentPage{}, err
	}
	defer cursor.Close(ctx)
	return readPage(ctx, cursor, offset, limit)
}

func (s *Store) AggregateDocuments(ctx context.Context, database, collection, pipelineJSON string, offset uint64, limit int) (connection.DocumentPage, error) {
	if err := s.ensureOpen(); err != nil {
		return connection.DocumentPage{}, err
	}
	if len(pipelineJSON) > maxDocumentBytes {
		return connection.DocumentPage{}, errors.New("MongoDB aggregation pipeline exceeds 1 MiB limit")
	}
	var pipeline bson.A
	if err := bson.UnmarshalExtJSON([]byte(pipelineJSON), true, &pipeline); err != nil {
		return connection.DocumentPage{}, fmt.Errorf("invalid MongoDB aggregation pipeline: %w", err)
	}
	for _, stage := range pipeline {
		if _, ok := stage.(bson.D); !ok {
			if _, ok = stage.(bson.M); !ok {
				return connection.DocumentPage{}, errors.New("each MongoDB pipeline stage must be an object")
			}
		}
	}
	if err := validateReadOnlyPipeline(pipeline); err != nil {
		return connection.DocumentPage{}, err
	}
	if offset > math.MaxInt64 {
		return connection.DocumentPage{}, errors.New("MongoDB result offset is too large")
	}
	limit = bounded(limit)
	if offset > 0 {
		pipeline = append(pipeline, bson.D{{Key: "$skip", Value: int64(offset)}})
	}
	pipeline = append(pipeline, bson.D{{Key: "$limit", Value: int64(limit)}})
	cursor, err := s.client.Database(database).Collection(collection).Aggregate(ctx, pipeline, options.Aggregate().SetBatchSize(int32(limit)))
	if err != nil {
		return connection.DocumentPage{}, err
	}
	defer cursor.Close(ctx)
	return readPage(ctx, cursor, offset, limit)
}

func (s *Store) GetDocument(ctx context.Context, database, collection, idJSON string) (connection.Document, error) {
	if err := s.ensureOpen(); err != nil {
		return connection.Document{}, err
	}
	filter, err := idFilter(idJSON)
	if err != nil {
		return connection.Document{}, fmt.Errorf("invalid MongoDB document id: %w", err)
	}
	var raw bson.Raw
	err = s.client.Database(database).Collection(collection).FindOne(ctx, filter).Decode(&raw)
	if err != nil {
		return connection.Document{}, err
	}
	jsonBytes, err := bson.MarshalExtJSON(raw, true, false)
	if err != nil {
		return connection.Document{}, err
	}
	return documentFromJSON(raw, jsonBytes)
}

func (s *Store) ValidateDocument(documentJSON string) error {
	if strings.TrimSpace(documentJSON) == "" {
		return errors.New("MongoDB document JSON is required")
	}
	if len(documentJSON) > maxDocumentBytes {
		return errors.New("MongoDB document exceeds 1 MiB write limit")
	}
	if _, err := decodeObject(documentJSON); err != nil {
		return fmt.Errorf("invalid MongoDB document JSON: %w", err)
	}
	return nil
}

func (s *Store) InsertDocument(ctx context.Context, database, collection, documentJSON string) error {
	if err := s.checkWritable(); err != nil {
		return err
	}
	if err := s.ValidateDocument(documentJSON); err != nil {
		return err
	}
	document, err := decodeObject(documentJSON)
	if err != nil {
		return fmt.Errorf("invalid MongoDB document JSON: %w", err)
	}
	_, err = s.client.Database(database).Collection(collection).InsertOne(ctx, document)
	return err
}

func (s *Store) ReplaceDocument(ctx context.Context, database, collection, idJSON, documentJSON string) error {
	if err := s.checkWritable(); err != nil {
		return err
	}
	if err := s.ValidateDocument(documentJSON); err != nil {
		return err
	}
	filter, err := idFilter(idJSON)
	if err != nil {
		return fmt.Errorf("invalid selected MongoDB id: %w", err)
	}
	document, err := decodeObject(documentJSON)
	if err != nil {
		return fmt.Errorf("invalid replacement document JSON: %w", err)
	}
	originalID := filter[0].Value
	replacementID, ok := lookupD(document, "_id")
	if !ok {
		return errors.New("MongoDB replacement must retain its _id field")
	}
	a, err := marshalExtendedValue(originalID)
	if err != nil {
		return fmt.Errorf("encode selected MongoDB id: %w", err)
	}
	b, err := marshalExtendedValue(replacementID)
	if err != nil {
		return fmt.Errorf("encode replacement MongoDB id: %w", err)
	}
	if string(a) != string(b) {
		return errors.New("MongoDB replacement cannot change _id")
	}
	result, err := s.client.Database(database).Collection(collection).ReplaceOne(ctx, filter, document)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

func (s *Store) ensureOpen() error {
	if s == nil || s.client == nil {
		return errors.New("MongoDB connection is closed")
	}
	return nil
}
func (s *Store) checkWritable() error {
	if err := s.ensureOpen(); err != nil {
		return err
	}
	if s.readOnly {
		return errors.New("MongoDB profile is read-only")
	}
	return nil
}
func decodeObject(text string) (bson.D, error) {
	if len(text) > maxDocumentBytes {
		return nil, errors.New("MongoDB JSON input exceeds 1 MiB limit")
	}
	if strings.TrimSpace(text) == "" {
		return bson.D{}, nil
	}
	var document bson.D
	err := bson.UnmarshalExtJSON([]byte(text), true, &document)
	return document, err
}
func idFilter(text string) (bson.D, error) {
	var wrapper bson.D
	err := bson.UnmarshalExtJSON([]byte(`{"id":`+text+`}`), true, &wrapper)
	if err != nil {
		return nil, err
	}
	if len(wrapper) != 1 {
		return nil, errors.New("MongoDB id must be one JSON value")
	}
	return bson.D{{Key: "_id", Value: wrapper[0].Value}}, nil
}

func lookupD(document bson.D, key string) (any, bool) {
	for _, element := range document {
		if element.Key == key {
			return element.Value, true
		}
	}
	return nil, false
}

func excludedID(projection bson.D) bool {
	value, ok := lookupD(projection, "_id")
	if !ok {
		return false
	}
	switch v := value.(type) {
	case bool:
		return !v
	case int32:
		return v == 0
	case int64:
		return v == 0
	case int:
		return v == 0
	case float64:
		return v == 0
	}
	return false
}

func validateReadOnlyPipeline(pipeline bson.A) error {
	for _, stage := range pipeline {
		if err := inspectPipelineValue(stage); err != nil {
			return err
		}
	}
	return nil
}
func inspectPipelineValue(value any) error {
	switch typed := value.(type) {
	case bson.D:
		for _, element := range typed {
			if element.Key == "$out" || element.Key == "$merge" {
				return fmt.Errorf("MongoDB aggregation stage %s writes to a collection and is disabled", element.Key)
			}
			if err := inspectPipelineValue(element.Value); err != nil {
				return err
			}
		}
	case bson.M:
		for key, nested := range typed {
			if key == "$out" || key == "$merge" {
				return fmt.Errorf("MongoDB aggregation stage %s writes to a collection and is disabled", key)
			}
			if err := inspectPipelineValue(nested); err != nil {
				return err
			}
		}
	case bson.A:
		for _, nested := range typed {
			if err := inspectPipelineValue(nested); err != nil {
				return err
			}
		}
	case []any:
		for _, nested := range typed {
			if err := inspectPipelineValue(nested); err != nil {
				return err
			}
		}
	}
	return nil
}
func bounded(n int) int {
	if n < 1 {
		return 50
	}
	if n > maxPageSize {
		return maxPageSize
	}
	return n
}
func readPage(ctx context.Context, cursor *mongo.Cursor, offset uint64, limit int) (connection.DocumentPage, error) {
	page := connection.DocumentPage{Documents: make([]connection.Document, 0, limit)}
	for cursor.Next(ctx) {
		raw := append(bson.Raw(nil), cursor.Current...)
		jsonBytes, err := bson.MarshalExtJSON(raw, true, false)
		if err != nil {
			return connection.DocumentPage{}, err
		}
		doc, err := documentFromJSON(raw, jsonBytes)
		if err != nil {
			return connection.DocumentPage{}, err
		}
		preview := jsonBytes
		if len(preview) > maxPreviewBytes {
			preview = preview[:maxPreviewBytes]
			doc.Truncated = true
		}
		doc.PreviewJSON = string(preview)
		doc.JSON = ""
		page.Documents = append(page.Documents, doc)
	}
	if err := cursor.Err(); err != nil {
		return connection.DocumentPage{}, err
	}
	page.NextOffset = offset + uint64(len(page.Documents))
	page.HasMore = len(page.Documents) == limit
	return page, nil
}
func documentFromJSON(raw bson.Raw, jsonBytes []byte) (connection.Document, error) {
	value, err := raw.LookupErr("_id")
	if err != nil {
		return connection.Document{}, fmt.Errorf("MongoDB result is missing _id: %w", err)
	}
	var id any
	if err := value.Unmarshal(&id); err != nil {
		return connection.Document{}, fmt.Errorf("decode MongoDB _id: %w", err)
	}
	encoded, err := marshalExtendedValue(id)
	if err != nil {
		return connection.Document{}, fmt.Errorf("encode MongoDB _id: %w", err)
	}
	truncated := len(jsonBytes) > maxDocumentBytes
	if truncated {
		jsonBytes = jsonBytes[:maxDocumentBytes]
	}
	return connection.Document{IDJSON: string(encoded), JSON: string(jsonBytes), Truncated: truncated}, nil
}

func marshalExtendedValue(value any) ([]byte, error) {
	wrapped, err := bson.MarshalExtJSON(bson.D{{Key: "value", Value: value}}, true, false)
	if err != nil {
		return nil, err
	}
	var decoded struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(wrapped, &decoded); err != nil {
		return nil, err
	}
	if len(decoded.Value) == 0 {
		return nil, errors.New("MongoDB Extended JSON value is empty")
	}
	return decoded.Value, nil
}
func sanitizeError(operation string, err error) error { return fmt.Errorf("%s: %w", operation, err) }
