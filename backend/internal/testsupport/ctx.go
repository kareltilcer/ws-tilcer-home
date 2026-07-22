package testsupport

import (
	"context"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/reqctx"
)

// CtxUser returns a context carrying a user actor with the given id and roles,
// plus a request id, for exercising handlers and the audit sink in tests.
func CtxUser(userID string, roles ...string) context.Context {
	ctx := reqctx.WithActor(context.Background(), reqctx.Actor{
		UserID: userID,
		Type:   "user",
		Label:  userID,
		Roles:  roles,
	})
	return reqctx.WithRequest(ctx, reqctx.RequestInfo{
		RequestID: "test-req",
		IP:        "127.0.0.1",
		UserAgent: "test",
		Site:      "home",
	})
}
