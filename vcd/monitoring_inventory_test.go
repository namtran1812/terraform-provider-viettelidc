package vcd

import (
	"testing"

	"github.com/vmware/go-vcloud-director/v2/types/v56"
)

func TestMonitoringResourceFromVM(t *testing.T) {
	vm := &types.QueryResultVMRecordType{
		ID:        "urn:vcloud:vm:123",
		HREF:      "https://vcd.example/api/vApp/vm-123",
		Name:      "application-01",
		IpAddress: "10.20.30.40",
		Deployed:  true,
		Status:    "POWERED_ON",
	}

	resource, ok := monitoringResourceFromVM(vm, "443")

	if !ok {
		t.Fatal("expected VM to be converted")
	}

	if resource.ID != "urn:vcloud:vm:123" {
		t.Fatalf("unexpected id: %s", resource.ID)
	}

	if resource.Name != "application-01" {
		t.Fatalf("unexpected name: %s", resource.Name)
	}

	if resource.Address != "10.20.30.40:443" {
		t.Fatalf("unexpected address: %s", resource.Address)
	}

	if resource.Type != "vcd_vm" {
		t.Fatalf("unexpected type: %s", resource.Type)
	}
}

func TestMonitoringResourceFromVMSkipsMissingIP(t *testing.T) {
	vm := &types.QueryResultVMRecordType{
		ID:   "urn:vcloud:vm:123",
		HREF: "https://vcd.example/api/vApp/vm-123",
		Name: "application-01",
	}

	_, ok := monitoringResourceFromVM(vm, "443")

	if ok {
		t.Fatal("expected VM without IP to be skipped")
	}
}

func TestMonitoringResourceFromVMUsesDefaultPort(t *testing.T) {
	vm := &types.QueryResultVMRecordType{
		ID:        "urn:vcloud:vm:123",
		HREF:      "https://vcd.example/api/vApp/vm-123",
		Name:      "application-01",
		IpAddress: "10.20.30.40",
	}

	resource, ok := monitoringResourceFromVM(vm, "")

	if !ok {
		t.Fatal("expected VM to be converted")
	}

	if resource.Address != "10.20.30.40:22" {
		t.Fatalf("unexpected address: %s", resource.Address)
	}
}
