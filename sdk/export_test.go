package sdk

// ReusePack is reusePack, for the tests of package sdk_test that cannot be in
// package sdk because they run the CLI, and internal/cli imports this package.
const ReusePack = reusePack
