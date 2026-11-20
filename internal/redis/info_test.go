package redis

import "testing"

func TestParseRedisInfoPreservesSectionsAndValueColons(t *testing.T) {
	raw := "# Server\r\nredis_version:8.0.1\r\nconfig_file:C:\\redis.conf\r\n\r\n# Keyspace\r\ndb0:keys=2,expires=0,avg_ttl=0\r\n"
	sections := parseRedisInfo(raw)
	if len(sections) != 2 || sections[0].Name != "Server" || sections[1].Name != "Keyspace" ||
		len(sections[0].Fields) != 2 || sections[0].Fields[1].Value != "C:\\redis.conf" ||
		len(sections[1].Fields) != 1 || sections[1].Fields[0].Value != "keys=2,expires=0,avg_ttl=0" {
		t.Fatalf("unexpected INFO sections: %+v", sections)
	}
}
