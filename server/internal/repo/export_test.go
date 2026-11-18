package repo

// AcquireFileForTest exposes admission without a second write operation.
var AcquireFileForTest = acquireFile

// StageFileForTest creates the real fsynced temporary file for commit tests.
var StageFileForTest = writeTemp

// WriteAtomicForTest exposes per-call filesystem seams without global hooks.
var WriteAtomicForTest = writeAtomicUsing

// RemoveSharedUsingForTest exposes the canonical delete commit with a real lease barrier.
var RemoveSharedUsingForTest = (*Repository).removeSharedFileUsing

// WithFileForTest acquires the production file lease for commit-boundary tests.
var WithFileForTest = withFile
