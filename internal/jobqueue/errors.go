package jobqueue

import "errors"

type PermanentError struct {
	err error
}

func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &PermanentError{err: err}
}

func (err *PermanentError) Error() string {
	return err.err.Error()
}

func (err *PermanentError) Unwrap() error {
	return err.err
}

func IsPermanent(err error) bool {
	var permanent *PermanentError
	return errors.As(err, &permanent)
}
