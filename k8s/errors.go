package k8s

import (
	"errors"

	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
)

type ErrReason struct {
	message, reason string
}

func (e ErrReason) Error() string {
	return e.message + ": " + e.reason
}

var ErrForbidden = errors.New("Forbidden")

func NormalizeError(err error) error {
	var statusErr *k8sErrors.StatusError
	if errors.As(err, &statusErr) {
		return ErrReason{statusErr.Error(), string(statusErr.ErrStatus.Reason)}
	}

	return err
}
