package store

import (
	"context"
	"time"
)

// AfterSendTimeout bounds the writes that record a message already sent.
const AfterSendTimeout = 5 * time.Second

// AfterSend returns the context for recording a message that has already
// been sent to Telegram. It is not cancelled with ctx: a SIGTERM (systemctl
// stop, a TimeoutStartSec expiry, a reboot) arriving between the send and the
// write would otherwise lose the record, and the next run would post the
// same message again. It still ends after AfterSendTimeout, so a stuck
// database cannot hold up shutdown.
func AfterSend(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), AfterSendTimeout)
}
