package installer

// GeneratorFingerprint identifies this installer revision. L1 D3 extends the
// original release fingerprint with the three rollback-related shell artifacts
// (NUL, name, NUL, bytes in name order), then takes 32 SHA256 hex digits. Stored
// links from the older, unsafe installer revision are served as StaleScript.
// TestGeneratorFingerprintMatchesReleaseInputs checks every revision input.
const GeneratorFingerprint = "52ee8a8a932df7dcd1ad4522f13097e8"

const legacyInstallerFingerprint = "4c01744e968c8fa791638b5943d5a61c"
