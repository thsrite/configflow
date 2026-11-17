package repo

// AcquireFileForTest exposes admission without a second write operation.
var AcquireFileForTest = acquireFile

// StageFileForTest creates the real fsynced temporary file for commit tests.
var StageFileForTest = writeTemp

// WriteAtomicForTest exposes per-call filesystem seams without global hooks.
var WriteAtomicForTest = writeAtomicUsing
