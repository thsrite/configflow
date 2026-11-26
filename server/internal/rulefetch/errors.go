package rulefetch

import "encoding/base64"

// basicAuthHeader encodes HTTP Basic credentials using ISO-8859-1.
func basicAuthHeader(user, password string) (string, error) {
	for _, r := range user + password {
		if r > 255 {
			return "", &credentialError{}
		}
	}
	return "Basic " + base64.StdEncoding.EncodeToString(latin1Bytes(user+":"+password)), nil
}

type credentialError struct{}

func (*credentialError) Error() string {
	return "HTTP Basic credentials must use ISO-8859-1 characters"
}
func (*credentialError) ErrorClass() string { return "CredentialEncodingError" }

// ErrorClass returns the stable rule-validation diagnostic label.
func (e *RemoteRuleValidationError) ErrorClass() string { return "RemoteRuleValidationError" }

type certificateBundleError struct{ err error }

func (e *certificateBundleError) Error() string {
	return "invalid TLS CA certificate bundle: " + e.err.Error()
}
func (e *certificateBundleError) Unwrap() error      { return e.err }
func (e *certificateBundleError) ErrorClass() string { return "CertificateBundleError" }
