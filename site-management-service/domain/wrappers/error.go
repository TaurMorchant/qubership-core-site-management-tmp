package errors

type ErrorWrapper struct {
	StatusCode int
	Message    string
}

func (e ErrorWrapper) Error() string {
	return e.Message
}
