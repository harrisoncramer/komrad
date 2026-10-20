package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadSignIn covers the provider an engine trusts, which has to be named whole, reached over
// https unless it runs on this machine, and served behind a loopback or private listener.
func TestLoadSignIn(t *testing.T) {
	type loadSignInCase struct {
		name    string
		file    string
		wantErr string
		want    SignIn
	}

	tests := []loadSignInCase{
		{
			name:    "an issuer with no client id",
			file:    "[sign_in]\nissuer = \"https://accounts.example.com\"\n",
			wantErr: "sign_in",
		},
		{
			name:    "a client id with no issuer",
			file:    "[sign_in]\nclient_id = \"komrad\"\n",
			wantErr: "sign_in",
		},
		{
			name:    "an issuer over plain http on the network",
			file:    "[sign_in]\nissuer = \"http://accounts.example.com\"\nclient_id = \"komrad\"\n",
			wantErr: "sign_in.issuer",
		},
		{
			name:    "a listener on every interface",
			file:    "[sign_in]\nissuer = \"https://accounts.example.com\"\nclient_id = \"komrad\"\naddr = \"0.0.0.0:8788\"\n",
			wantErr: "sign_in.addr",
		},
		{
			name: "no provider, which takes no signed-in requests",
			file: "",
			want: SignIn{
				Addr: defaultSignInAddr,
			},
		},
		{
			name: "an issuer over plain http on this machine",
			file: "[sign_in]\nissuer = \"http://127.0.0.1:9000\"\nclient_id = \"komrad\"\n",
			want: SignIn{
				Issuer:   "http://127.0.0.1:9000",
				ClientID: "komrad",
				Addr:     defaultSignInAddr,
			},
		},
		{
			name: "a listener on a private address, behind the control plane",
			file: "[sign_in]\nissuer = \"https://accounts.example.com\"\nclient_id = \"komrad\"\naddr = \"10.42.0.20:8788\"\n",
			want: SignIn{
				Issuer:   "https://accounts.example.com",
				ClientID: "komrad",
				Addr:     "10.42.0.20:8788",
			},
		},
		{
			name: "an https issuer on its own listener",
			file: "[sign_in]\nissuer = \"https://accounts.example.com\"\nclient_id = \"komrad\"\nclient_secret = \"secret\"\naddr = \"127.0.0.1:9000\"\n",
			want: SignIn{
				Issuer:       "https://accounts.example.com",
				ClientID:     "komrad",
				ClientSecret: "secret",
				Addr:         "127.0.0.1:9000",
			},
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
			assert.Equal(t, tt.want, cfg.SignIn)
		})
	}
}
