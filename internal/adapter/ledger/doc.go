// Package ledger holds the file-based adapters of the outlook CLI: the
// idempotency and send-history Ledger, and the attachment QuarantineStore.
//
// The ledger is one JSON file guarded by an advisory file lock (flock on a
// sibling .lock file) and replaced atomically (write temp, fsync, rename), so
// two CLI processes cannot both reserve a key. It never stores message
// bodies: only the key, a fingerprint, status, time and ids. It fails closed:
// a corrupt, unreadable or future-versioned file is an error and the caller
// must not send. Unix only (darwin, linux); native Windows is out of scope.
package ledger
