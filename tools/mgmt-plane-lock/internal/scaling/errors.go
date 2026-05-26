package scaling

import "errors"

// errNilClientset is returned by Runner.Run when the caller forgot to
// wire a Kubernetes client. Programmer error; surfaced as an error so
// the caller can log + exit rather than panic.
var errNilClientset = errors.New("scaling: Runner.Clientset is nil")
