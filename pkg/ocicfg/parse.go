package ocicfg

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Profile holds minimal OCI CLI profile fields we need.
type Profile struct {
	User    string
	Tenancy string
	Region  string
}

// LoadProfiles parses the OCI CLI config (~/.oci/config) and returns profiles.
// Missing user is tolerated (session auth); missing tenancy or region remains an error.
func LoadProfiles(path string) (map[string]Profile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	profiles := make(map[string]Profile)
	var current string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.TrimSpace(line[1 : len(line)-1])
			if _, exists := profiles[current]; !exists {
				profiles[current] = Profile{}
			}
			continue
		}
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 || current == "" {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		p := profiles[current]
		switch key {
		case "user":
			p.User = val
		case "tenancy":
			p.Tenancy = val
		case "region":
			p.Region = val
		}
		profiles[current] = p
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// validate (tenancy and region required; user optional for session auth)
	for name, p := range profiles {
		if p.Tenancy == "" {
			return nil, fmt.Errorf("profile %s missing tenancy", name)
		}
		if p.Region == "" {
			return nil, fmt.Errorf("profile %s missing region", name)
		}
		if p.User == "" {
			p.User = p.Tenancy // placeholder for session auth
			profiles[name] = p
		}
	}

	return profiles, nil
}
