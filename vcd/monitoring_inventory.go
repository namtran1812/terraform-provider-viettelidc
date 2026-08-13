package vcd

import (
	"context"
	"fmt"
	"net"

	"github.com/vmware/go-vcloud-director/v2/types/v56"
	"github.com/vmware/terraform-provider-vcd/v3/internal/providerinventory"
)

// VCDInventorySource adapts infrastructure managed by the existing
// VMware Cloud Director provider into the monitoring control plane.
//
// It deliberately reuses the provider's authenticated VCDClient and the same
// deployed-VM query path used elsewhere in the provider.
type VCDInventorySource struct {
	Client *VCDClient

	Org string
	Vdc string

	// Port is the TCP port monitored for discovered VMs.
	// For local/demo usage this can be set with MONITOR_PORT.
	Port string
}

func (s VCDInventorySource) ListResources(
	ctx context.Context,
) ([]providerinventory.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if s.Client == nil {
		return nil, fmt.Errorf("nil VCD client")
	}

	_, vdc, err := s.Client.GetOrgAndVdc(
		s.Org,
		s.Vdc,
	)
	if err != nil {
		return nil, fmt.Errorf("resolve provider org/vdc: %w", err)
	}

	records, err := vdc.QueryVmList(
		types.VmQueryFilterOnlyDeployed,
	)
	if err != nil {
		return nil, fmt.Errorf("query deployed VMs: %w", err)
	}

	resources := make(
		[]providerinventory.Resource,
		0,
		len(records),
	)

	for _, record := range records {
		resource, ok := monitoringResourceFromVM(
			record,
			s.Port,
		)
		if !ok {
			continue
		}

		resources = append(resources, resource)
	}

	return resources, nil
}

func monitoringResourceFromVM(
	vm *types.QueryResultVMRecordType,
	port string,
) (providerinventory.Resource, bool) {
	if vm == nil {
		return providerinventory.Resource{}, false
	}

	if vm.HREF == "" || vm.Name == "" || vm.IpAddress == "" {
		return providerinventory.Resource{}, false
	}

	if port == "" {
		port = "22"
	}

	id := vm.ID
	if id == "" {
		id = "urn:vcloud:vm:" + extractUuid(vm.HREF)
	}

	return providerinventory.Resource{
		ID:      id,
		Name:    vm.Name,
		Address: net.JoinHostPort(vm.IpAddress, port),
		Type:    "vcd_vm",
	}, true
}
