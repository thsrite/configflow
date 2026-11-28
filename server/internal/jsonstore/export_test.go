package jsonstore

// SetPublicationHooksForTest pauses actual I/O without substituting a writer.
func SetPublicationHooksForTest(s *Store, afterTempWrite, beforeCommit func(), afterReplace func(string)) {
	s.afterTempWrite, s.beforeCommit, s.afterReplace = afterTempWrite, beforeCommit, afterReplace
}

// FileSignatureForTest returns the same file identity used by signed writes.
func FileSignatureForTest(path string) (Signature, error) {
	return FileSignature(path)
}
