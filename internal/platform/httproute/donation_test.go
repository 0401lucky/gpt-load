package httproute

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDonationOwnerRequiresItsDedicatedAuthentication(t *testing.T) {
	t.Parallel()
	module := Module{Name: "donation", Owner: OwnerDonation, Auth: AuthDonation,
		Prefix: "/integrations/donations/v1", NamespacePrefixes: []string{"/integrations/donations"},
		Authenticate: func(*gin.Context) {}, Routes: []Route{{Name: "donation.capabilities.get", Path: "/capabilities",
			Methods: []string{http.MethodGet}, Handlers: gin.HandlersChain{func(*gin.Context) {}}}}}
	if _, err := NewRegistry(module); err != nil {
		t.Fatal(err)
	}
	for _, auth := range []AuthPolicy{AuthNone, AuthControl, AuthAccessKey} {
		module.Auth = auth
		if _, err := NewRegistry(module); err == nil {
			t.Errorf("donation owner accepted auth policy %q", auth)
		}
	}
	module.Auth, module.Authenticate = AuthDonation, nil
	if _, err := NewRegistry(module); err == nil {
		t.Fatal("donation module accepted missing authenticator")
	}
}
