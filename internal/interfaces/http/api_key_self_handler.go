package http

import (
	stdhttp "net/http"

	"github.com/gin-gonic/gin"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// apiKeySelfResponse is the safe introspection payload returned to a Bearer
// caller. It never contains the key value, hash, ciphertext, or any user data.
type apiKeySelfResponse struct {
	Scopes []string `json:"scopes"`
}

type apiKeySelfHandler struct{}

// get returns the authenticated API key's scope strings. It rejects Session
// auth (403) and serializes authCtx.Scopes for Bearer auth. It is used by
// downstream consumers (e.g. jinshu connection testing) to verify the key holds
// the required scope subset, without ever returning the key value.
func (h apiKeySelfHandler) get(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", domainerrors.ErrForbidden.Error())
		return
	}
	if !authCtx.IsAPIKey() {
		writeError(c, stdhttp.StatusForbidden, "forbidden", domainerrors.ErrForbidden.Error())
		return
	}
	scopes := make([]string, 0, len(authCtx.Scopes))
	for _, scope := range authCtx.Scopes {
		scopes = append(scopes, string(scope))
	}
	c.JSON(stdhttp.StatusOK, apiKeySelfResponse{Scopes: scopes})
}
