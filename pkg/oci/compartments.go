package oci

import (
	"context"
	"fmt"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/identity"
)

// Compartment represents a simplified compartment record.
type Compartment struct {
	ID     string
	Name   string
	Status string
	Parent string
}

// FetchCompartments fetches direct child compartments for parentID.
// profileConfigPath: OCI config file path (e.g., ~/.oci/config)
// profile: profile name
// region: region to target
// parentID: compartment or tenancy OCID
func FetchCompartments(ctx context.Context, profileConfigPath, profile, region, parentID string) ([]Compartment, error) {
	if profileConfigPath == "" {
		return nil, fmt.Errorf("oci config path required")
	}
	provider, err := common.ConfigurationProviderFromFileWithProfile(profileConfigPath, profile, "")
	if err != nil {
		return nil, fmt.Errorf("config provider: %w", err)
	}
	client, err := identity.NewIdentityClientWithConfigurationProvider(provider)
	if err != nil {
		return nil, fmt.Errorf("identity client: %w", err)
	}
	if region != "" {
		client.SetRegion(region)
	}

	req := identity.ListCompartmentsRequest{
		CompartmentId:          common.String(parentID),
		CompartmentIdInSubtree: common.Bool(false),
		Limit:                  common.Int(1000),
	}

	var out []Compartment
	for {
		resp, err := client.ListCompartments(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("list compartments: %w", err)
		}
		for _, c := range resp.Items {
			out = append(out, Compartment{
				ID:     *c.Id,
				Name:   deref(c.Name),
				Status: string(c.LifecycleState),
				Parent: deref(c.CompartmentId),
			})
		}
		if resp.OpcNextPage == nil || *resp.OpcNextPage == "" {
			break
		}
		req.Page = resp.OpcNextPage
	}
	return out, nil
}

func deref(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}
