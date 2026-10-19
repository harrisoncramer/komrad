package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadControlPlane covers the control plane's listeners, its CA and the engines it takes
// requests from.
func TestLoadControlPlane(t *testing.T) {
	tests := []struct {
		name        string
		file        string
		wantErr     string
		want        ControlPlane
		wantEngines map[string]string
	}{
		{
			name:        "nothing set listens on loopback",
			want:        ControlPlane{Addr: defaultControlPlaneAddr, ForgeProxyAddr: defaultForgeProxyAddr},
			wantEngines: map[string]string{},
		},
		{
			name: "a forge proxy on a private address with two engines",
			file: "[control_plane]\nforge_proxy_addr = \"10.0.1.5:8789\"\nca_cert_file = \"/etc/komrad/ca.pem\"\nca_key_file = \"/etc/komrad/ca.key\"\nengines = [\"harry=6f1c\", \" ada = 9b2e \"]\n",
			want: ControlPlane{
				Addr:           defaultControlPlaneAddr,
				ForgeProxyAddr: "10.0.1.5:8789",
				CACertFile:     "/etc/komrad/ca.pem",
				CAKeyFile:      "/etc/komrad/ca.key",
				Engines:        []string{"harry=6f1c", " ada = 9b2e "},
			},
			wantEngines: map[string]string{"harry": "6f1c", "ada": "9b2e"},
		},
		{
			name:    "endpoints the network reaches directly",
			file:    "[control_plane]\naddr = \"0.0.0.0:8790\"\n",
			wantErr: "control_plane.addr",
		},
		{
			name:    "a forge proxy on a public address",
			file:    "[control_plane]\nforge_proxy_addr = \"203.0.113.7:8789\"\n",
			wantErr: "control_plane.forge_proxy_addr",
		},
		{
			name:    "a CA certificate with no key",
			file:    "[control_plane]\nca_cert_file = \"/etc/komrad/ca.pem\"\n",
			wantErr: "ca_key_file",
		},
		{
			name:    "an engine with no credential",
			file:    "[control_plane]\nengines = [\"harry\"]\n",
			wantErr: "control_plane.engines",
		},
		{
			name:    "an engine name that would break the forge proxy url",
			file:    "[control_plane]\nengines = [\"harry:x=6f1c\"]\n",
			wantErr: "control_plane.engines",
		},
		{
			name:    "an engine named twice",
			file:    "[control_plane]\nengines = [\"harry=6f1c\", \"harry=9b2e\"]\n",
			wantErr: "named a second time",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loadStrict(t, tt.file)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.ControlPlane)

			engines, err := cfg.ControlPlane.GetEngines()
			require.NoError(t, err)
			assert.Equal(t, tt.wantEngines, engines)
		})
	}
}

// TestLoadForgeProxy covers the forge proxy an engine names, which has to carry the engine's
// name and credential and come with the CA to trust.
func TestLoadForgeProxy(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		wantErr string
		want    ForgeProxy
	}{
		{
			name: "no forge proxy",
		},
		{
			name: "a forge proxy with the engine's name and credential",
			file: "[forge_proxy]\nurl = \" http://harry:6f1c@10.0.1.5:8789 \"\nca_file = \"~/.config/komrad/ca.pem\"\n",
			want: ForgeProxy{URL: "http://harry:6f1c@10.0.1.5:8789", CAFile: "~/.config/komrad/ca.pem"},
		},
		{
			name:    "a CA with no forge proxy",
			file:    "[forge_proxy]\nca_file = \"/etc/komrad/ca.pem\"\n",
			wantErr: "url is empty",
		},
		{
			name:    "a forge proxy over https",
			file:    "[forge_proxy]\nurl = \"https://harry:6f1c@10.0.1.5:8789\"\nca_file = \"/etc/komrad/ca.pem\"\n",
			wantErr: "forge_proxy.url",
		},
		{
			name:    "no engine name",
			file:    "[forge_proxy]\nurl = \"http://10.0.1.5:8789\"\nca_file = \"/etc/komrad/ca.pem\"\n",
			wantErr: "names no engine",
		},
		{
			name:    "no forge proxy credential",
			file:    "[forge_proxy]\nurl = \"http://harry@10.0.1.5:8789\"\nca_file = \"/etc/komrad/ca.pem\"\n",
			wantErr: "no forge proxy credential",
		},
		{
			name:    "no CA to trust",
			file:    "[forge_proxy]\nurl = \"http://harry:6f1c@10.0.1.5:8789\"\n",
			wantErr: "ca_file is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loadStrict(t, tt.file)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.ForgeProxy)
			assert.Equal(t, tt.want.URL != "", cfg.ForgeProxy.IsConfigured())
		})
	}
}
