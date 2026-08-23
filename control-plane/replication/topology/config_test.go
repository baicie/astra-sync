package topology

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

const testTopologyYAML = `
regions:
  - name: us-east-1
    role: primary
    apiServerEndpoint: https://api.us-east-1.astrasync.example
    postgresEndpoint: postgresql://pg.us-east-1.astrasync.example:5432
    objectStorageBucket: astrasync-checkpoints-us-east-1
    objectStoragePrefix: checkpoints/
  - name: eu-west-1
    role: standby
    apiServerEndpoint: https://api.eu-west-1.astrasync.example
    postgresEndpoint: postgresql://pg.eu-west-1.astrasync.example:5432
    objectStorageBucket: astrasync-checkpoints-eu-west-1
    objectStoragePrefix: checkpoints/
replicationLagThresholdSec: 5
`

func TestParseTopology(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	loader, err := parseTopology(logger, []byte(testTopologyYAML), 1)
	if err != nil {
		t.Fatalf("parseTopology failed: %v", err)
	}

	if len(loader.config.Regions) != 2 {
		t.Errorf("expected 2 regions, got %d", len(loader.config.Regions))
	}

	if loader.config.ReplicationLagThresholdSec != 5 {
		t.Errorf("expected lag threshold 5, got %d", loader.config.ReplicationLagThresholdSec)
	}
}

func TestGetRegion(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	loader, err := parseTopology(logger, []byte(testTopologyYAML), 1)
	if err != nil {
		t.Fatalf("parseTopology failed: %v", err)
	}

	region, err := loader.GetRegion("us-east-1")
	if err != nil {
		t.Fatalf("GetRegion failed: %v", err)
	}

	if region.Role != RegionRolePrimary {
		t.Errorf("expected primary role, got %s", region.Role)
	}
}

func TestGetRegion_NotFound(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	loader, err := parseTopology(logger, []byte(testTopologyYAML), 1)
	if err != nil {
		t.Fatalf("parseTopology failed: %v", err)
	}

	_, err = loader.GetRegion("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent region")
	}
}

func TestGetPrimaryRegion(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	loader, err := parseTopology(logger, []byte(testTopologyYAML), 1)
	if err != nil {
		t.Fatalf("parseTopology failed: %v", err)
	}

	region, err := loader.GetPrimaryRegion()
	if err != nil {
		t.Fatalf("GetPrimaryRegion failed: %v", err)
	}

	if region.Name != "us-east-1" {
		t.Errorf("expected primary region us-east-1, got %s", region.Name)
	}
}

func TestGetStandbyRegions(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	loader, err := parseTopology(logger, []byte(testTopologyYAML), 1)
	if err != nil {
		t.Fatalf("parseTopology failed: %v", err)
	}

	standbys := loader.GetStandbyRegions()
	if len(standbys) != 1 {
		t.Errorf("expected 1 standby region, got %d", len(standbys))
	}

	if standbys[0].Name != "eu-west-1" {
		t.Errorf("expected standby eu-west-1, got %s", standbys[0].Name)
	}
}

func TestIsPrimary(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	loader, err := parseTopology(logger, []byte(testTopologyYAML), 1)
	if err != nil {
		t.Fatalf("parseTopology failed: %v", err)
	}

	if !loader.IsPrimary("us-east-1") {
		t.Error("us-east-1 should be primary")
	}

	if loader.IsPrimary("eu-west-1") {
		t.Error("eu-west-1 should not be primary")
	}
}

func TestIsStandby(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	loader, err := parseTopology(logger, []byte(testTopologyYAML), 1)
	if err != nil {
		t.Fatalf("parseTopology failed: %v", err)
	}

	if !loader.IsStandby("eu-west-1") {
		t.Error("eu-west-1 should be standby")
	}

	if loader.IsStandby("us-east-1") {
		t.Error("us-east-1 should not be standby")
	}
}

func TestParseTopology_EmptyRegions(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	_, err := parseTopology(logger, []byte("regions: []"), 1)
	if err == nil {
		t.Error("expected error for empty regions")
	}
}

func TestParseTopology_DuplicateRegion(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	yaml := `
regions:
  - name: us-east-1
    role: primary
  - name: us-east-1
    role: standby
`
	_, err := parseTopology(logger, []byte(yaml), 1)
	if err == nil {
		t.Error("expected error for duplicate region")
	}
}

func TestParseTopology_RequiresExactlyOnePrimary(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	for name, yaml := range map[string]string{
		"no_primary": `
regions:
  - name: eu-west-1
    role: standby
    apiServerEndpoint: https://eu
    postgresEndpoint: postgres://eu
    objectStorageBucket: bucket
    objectStoragePrefix: wal
`, "multiple_primary": `
regions:
  - name: us-east-1
    role: primary
    apiServerEndpoint: https://east
    postgresEndpoint: postgres://east
    objectStorageBucket: bucket-east
    objectStoragePrefix: wal
  - name: us-west-1
    role: primary
    apiServerEndpoint: https://west
    postgresEndpoint: postgres://west
    objectStorageBucket: bucket-west
    objectStoragePrefix: wal
`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseTopology(logger, []byte(yaml), 1)
			if err == nil {
				t.Fatal("expected topology validation error")
			}
		})
	}
}

func TestParseTopology_RejectsUnknownRoleAndMissingEndpoint(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	unknownRole := []byte(`
regions:
  - name: us-east-1
    role: active
    apiServerEndpoint: https://east
    postgresEndpoint: postgres://east
    objectStorageBucket: bucket
    objectStoragePrefix: wal
`)
	if _, err := parseTopology(logger, unknownRole, 1); err == nil {
		t.Fatal("expected unknown role error")
	}
	missingEndpoint := []byte(`
regions:
  - name: us-east-1
    role: primary
    postgresEndpoint: postgres://east
    objectStorageBucket: bucket
    objectStoragePrefix: wal
`)
	if _, err := parseTopology(logger, missingEndpoint, 1); err == nil {
		t.Fatal("expected missing endpoint error")
	}
}

func TestNewFileLoader(t *testing.T) {
	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "topology-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(testTopologyYAML); err != nil {
		t.Fatalf("failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	logger, _ := zap.NewDevelopment()
	loader, err := NewFileLoader(context.Background(), logger, tmpFile.Name())
	if err != nil {
		t.Fatalf("NewFileLoader failed: %v", err)
	}

	regions := loader.GetAllRegions()
	if len(regions) != 2 {
		t.Errorf("expected 2 regions, got %d", len(regions))
	}
}

func TestNewLoader_Defaults(t *testing.T) {
	// Outside Kubernetes, NewLoader must load a concrete local topology file.
	path := filepath.Join(t.TempDir(), "regions.yaml")
	if err := os.WriteFile(path, []byte(testTopologyYAML), 0600); err != nil {
		t.Fatalf("write topology file: %v", err)
	}
	logger, _ := zap.NewDevelopment()
	loader, err := NewLoader(context.Background(), logger, path, "", "")
	if err != nil {
		t.Fatalf("NewLoader failed: %v", err)
	}

	if loader.namespace != "astrasync" {
		t.Errorf("expected namespace 'astrasync', got %s", loader.namespace)
	}

	if loader.key != "regions.yaml" {
		t.Errorf("expected key 'regions.yaml', got %s", loader.key)
	}
}

func TestGetConfig(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	loader, err := parseTopology(logger, []byte(testTopologyYAML), 1)
	if err != nil {
		t.Fatalf("parseTopology failed: %v", err)
	}

	cfg := loader.GetConfig()
	if cfg == nil {
		t.Fatal("GetConfig returned nil")
	}

	// Verify it's a copy, not the original
	cfg.Regions[0].Name = "modified"
	if loader.config.Regions[0].Name != "us-east-1" {
		t.Error("GetConfig should return a copy")
	}
}
