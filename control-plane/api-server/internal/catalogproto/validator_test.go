package catalogproto_test

import (
	"os"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/api-server/internal/catalogproto"
)

func TestValidatorAcceptsTheJavaPublishedDeploymentInventory(t *testing.T) {
	payload := deploymentInventory(t)
	snapshot, err := (catalogproto.Validator{}).Validate(payload, time.Date(2026, 8, 8, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("validate deployment inventory: %v", err)
	}
	if snapshot.InventoryRevision != "sha256:1d0247c24e0c30e78bdbd135e43f72a6c58a4d2680c0d59337ae69668b771778" ||
		len(snapshot.Descriptors) != 4 || snapshot.Descriptors[0].Name != "csv" ||
		snapshot.Descriptors[3].Name != "postgres-cdc" {
		t.Fatalf("unexpected deployment snapshot: %+v", snapshot)
	}
}

func TestValidatorRejectsDescriptorTamperingAndNonCanonicalBytes(t *testing.T) {
	inventory := &controlv1.ConnectorInventory{}
	if err := proto.Unmarshal(deploymentInventory(t), inventory); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	inventory.Descriptors[0].DisplayName = "Tampered"
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(inventory)
	if err != nil {
		t.Fatalf("encode tampered fixture: %v", err)
	}
	if _, err := (catalogproto.Validator{}).Validate(payload, time.Now()); err == nil {
		t.Fatal("expected descriptor revision mismatch")
	}

	canonical := deploymentInventory(t)
	nonCanonical := append([]byte{0x98, 0x06, 0x00}, canonical...)
	if _, err := (catalogproto.Validator{}).Validate(nonCanonical, time.Now()); err == nil {
		t.Fatal("expected unknown or non-canonical field rejection")
	}
}

func deploymentInventory(t *testing.T) []byte {
	t.Helper()
	payload, err := os.ReadFile("../../../../deployment/catalog/connector-inventory.pb")
	if err != nil {
		t.Fatalf("read deployment inventory: %v", err)
	}
	return payload
}
