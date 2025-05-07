package exceptions

type (
	OpenShiftPermissionError struct {
		message string
	}
)

func NewOpenShiftPermissionError(message string) OpenShiftPermissionError {
	return OpenShiftPermissionError{
		message: message,
	}
}

func (e OpenShiftPermissionError) Error() string {
	return e.message
}
