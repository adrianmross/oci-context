package cmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type authSubjectResult struct {
	Service    string `json:"service"`
	Context    string `json:"context,omitempty"`
	Issuer     string `json:"issuer"`
	Subject    string `json:"subject"`
	ExpiresAt  string `json:"expires_at"`
	NotExpired bool   `json:"not_expired"`
}

func newAuthSubjectCmd(resolvePath authResolvePathFunc, loadTarget authLoadTargetFunc) *cobra.Command {
	var service string
	var expectedIssuer string
	cmd := &cobra.Command{
		Use:   "subject",
		Short: "Emit metadata-only subject details for the cached access token",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolvePath(cmd)
			if err != nil {
				return err
			}
			cfg, ctx, err := loadTarget(path)
			if err != nil {
				return err
			}
			request, err := resolveTokenServiceRequest(cfg, tokenServiceOptions{Service: service})
			if err != nil {
				return err
			}
			cachePath, err := authTokenCachePath(cfg, request.Service, request.Issuer, request.ClientID, request.Scope)
			if err != nil {
				return err
			}
			entry, err := readAuthTokenCache(cachePath)
			if err != nil {
				return fmt.Errorf("read cached access token: %w", err)
			}
			result, err := subjectFromAccessToken(entry, firstNonEmpty(expectedIssuer, request.Issuer))
			if err != nil {
				return err
			}
			result.Context = ctx.Name
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		},
	}
	cmd.Flags().StringVar(&service, "service", "", "token service name (default current_service else obp)")
	cmd.Flags().StringVar(&expectedIssuer, "require-issuer", "", "expected JWT issuer; defaults to the token service issuer")
	return cmd
}

func subjectFromAccessToken(entry authTokenCacheEntry, expectedIssuer string) (authSubjectResult, error) {
	parts := strings.Split(entry.AccessToken, ".")
	if len(parts) != 3 {
		return authSubjectResult{}, fmt.Errorf("access token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return authSubjectResult{}, fmt.Errorf("decode JWT payload: %w", err)
	}
	var claims struct {
		Issuer  string      `json:"iss"`
		Subject string      `json:"sub"`
		Expires json.Number `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return authSubjectResult{}, fmt.Errorf("decode JWT claims: %w", err)
	}
	if strings.TrimSpace(expectedIssuer) == "" || claims.Issuer != expectedIssuer {
		return authSubjectResult{}, fmt.Errorf("JWT issuer does not match expected issuer")
	}
	if strings.TrimSpace(claims.Subject) == "" {
		return authSubjectResult{}, fmt.Errorf("JWT subject claim is required")
	}
	expiresUnix, err := claims.Expires.Int64()
	if err != nil {
		return authSubjectResult{}, fmt.Errorf("JWT expiration claim is invalid")
	}
	expiresAt := time.Unix(expiresUnix, 0).UTC()
	if !expiresAt.After(time.Now()) {
		return authSubjectResult{}, fmt.Errorf("JWT is expired")
	}
	return authSubjectResult{Service: entry.Service, Issuer: claims.Issuer, Subject: claims.Subject, ExpiresAt: expiresAt.Format(time.RFC3339), NotExpired: true}, nil
}
