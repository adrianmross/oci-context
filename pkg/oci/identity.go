package oci

import (
	"context"
	"fmt"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/identity"
)

// IdentityDetails holds friendly names for tenancy, compartment, and user.
type IdentityDetails struct {
	TenancyName     string
	TenancyOCID     string
	CompartmentName string
	CompartmentOCID string
	UserName        string
	UserOCID        string
	Region          string
}

// FetchIdentityDetails retrieves friendly names for tenancy, compartment, and user.
// tenancyOCID and userOCID may come from the OCI profile (provider tenancy & user),
// compartmentOCID is taken from context.
func FetchIdentityDetails(ctx context.Context, profileConfigPath, profile, region, tenancyOCID, compartmentOCID, userOCID string) (IdentityDetails, error) {
	if profileConfigPath == "" {
		return IdentityDetails{}, fmt.Errorf("oci config path required")
	}
	provider, err := common.ConfigurationProviderFromFileWithProfile(profileConfigPath, profile, "")
	if err != nil {
		return IdentityDetails{}, fmt.Errorf("config provider: %w", err)
	}
	// If tenancy/user not supplied, derive from provider
	if tenancyOCID == "" {
		tenancyOCID, err = provider.TenancyOCID()
		if err != nil {
			return IdentityDetails{}, fmt.Errorf("tenancy ocid: %w", err)
		}
	}
	if userOCID == "" {
		userOCID, err = provider.UserOCID()
		if err != nil {
			return IdentityDetails{}, fmt.Errorf("user ocid: %w", err)
		}
	}

	client, err := identity.NewIdentityClientWithConfigurationProvider(provider)
	if err != nil {
		return IdentityDetails{}, fmt.Errorf("identity client: %w", err)
	}
	if region != "" {
		client.SetRegion(region)
	}

	// tenancy name
	tenResp, err := client.GetTenancy(ctx, identity.GetTenancyRequest{TenancyId: common.String(tenancyOCID)})
	if err != nil {
		return IdentityDetails{}, fmt.Errorf("get tenancy: %w", err)
	}

	compName := ""
	if compartmentOCID != "" {
		compResp, err := client.GetCompartment(ctx, identity.GetCompartmentRequest{CompartmentId: common.String(compartmentOCID)})
		if err == nil {
			compName = deref(compResp.Name)
		}
	}

	usrResp, err := client.GetUser(ctx, identity.GetUserRequest{UserId: common.String(userOCID)})
	if err != nil {
		return IdentityDetails{}, fmt.Errorf("get user: %w", err)
	}

	return IdentityDetails{
		TenancyName:     deref(tenResp.Name),
		TenancyOCID:     tenancyOCID,
		CompartmentName: compName,
		CompartmentOCID: compartmentOCID,
		UserName:        deref(usrResp.Description),
		UserOCID:        userOCID,
		Region:          region,
	}, nil
}

// ListRegionSubscriptions returns the region names enabled for the tenancy (subscriptions).
// It uses the given OCI profile (and optional config path) and does not require a region to be set.
func ListRegionSubscriptions(ctx context.Context, profileConfigPath, profile string) ([]string, error) {
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

	tid, err := provider.TenancyOCID()
	if err != nil {
		return nil, fmt.Errorf("tenancy ocid: %w", err)
	}

	resp, err := client.ListRegionSubscriptions(ctx, identity.ListRegionSubscriptionsRequest{
		TenancyId: common.String(tid),
	})
	if err != nil {
		return nil, fmt.Errorf("list region subscriptions: %w", err)
	}

	regions := make([]string, 0, len(resp.Items))
	for _, r := range resp.Items {
		if r.RegionName != nil {
			regions = append(regions, *r.RegionName)
		}
	}
	return regions, nil
}
