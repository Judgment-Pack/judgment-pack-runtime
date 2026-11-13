package sdk

// ReusePack is reusePack, for the tests of package sdk_test that cannot be in
// package sdk because they run the CLI, and internal/cli imports this package.
const ReusePack = reusePack

// RecordPack030 and RecordPack050 are the decision-record fixtures, which the
// hand-written records of package sdk and the tests of package sdk_test share.
const (
	RecordPack030 = recordPack030
	RecordPack050 = recordPack050
)
