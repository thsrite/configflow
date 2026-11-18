package installer

// GeneratorFingerprint identifies this installer revision. L1 D3 extends the
// original release fingerprint with the rollback and credential-redaction shell artifacts
// (NUL, name, NUL, bytes in name order), then takes 32 SHA256 hex digits. Stored
// links from the older, unsafe installer revision are served as StaleScript.
// TestGeneratorFingerprintMatchesReleaseInputs checks every revision input.
const GeneratorFingerprint = "dcf4a56f20735e17e1f87a8a0ab8143e"

const legacyInstallerFingerprint = "4c01744e968c8fa791638b5943d5a61c"
