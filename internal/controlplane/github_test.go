package controlplane_test

import (
	"net/http"
	"testing"

	"komrad/internal/controlplane"

	"github.com/stretchr/testify/assert"
)

// TestTheControlPlaneSaysWhetherAnEnginesGitHubLoginWorks covers the answer the panel reads
// before it lets a person in, by what GitHub makes of the engine's login.
func TestTheControlPlaneSaysWhetherAnEnginesGitHubLoginWorks(t *testing.T) {
	tests := []struct {
		name           string
		person         string
		github         func(p *plane)
		wantStatus     int
		wantAuthorized bool
	}{
		{
			name:           "a login GitHub takes",
			person:         "harry",
			github:         func(*plane) {},
			wantStatus:     http.StatusOK,
			wantAuthorized: true,
		},
		{
			name:       "a login the person revoked",
			person:     "ada",
			github:     func(p *plane) { p.github.AnswerUser("gho_ada", http.StatusUnauthorized) },
			wantStatus: http.StatusOK,
		},
		{
			name:       "no login",
			person:     "grace",
			github:     func(*plane) {},
			wantStatus: http.StatusOK,
		},
		{
			name:       "GitHub failing",
			person:     "harry",
			github:     func(p *plane) { p.github.AnswerUser("gho_harry", http.StatusInternalServerError) },
			wantStatus: http.StatusBadGateway,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := startPlane(t)
			tt.github(p)

			var answer controlplane.GitHubStatus
			assert.Equal(t, tt.wantStatus, p.call(t, http.MethodGet, "/"+p.engine(tt.person).name+controlplane.GitHubPath, harryToken, &answer))
			assert.Equal(t, tt.wantAuthorized, answer.Authorized)
		})
	}
}
