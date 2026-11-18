# Registry line mutation table

Each diagnostic code of the registry line has one row here: the test that asserts the code, and a
recorded mutant, a change to product code that the test kills. A mutant is described, not stored
as a patch. A reviewer re-runs a row by hand: in a scratch copy of the tree, apply the mutant,
confirm the package still builds (`go vet` on it), then run only the row's test and confirm it
fails.

## Scope

The rows follow from the source; nobody lists the codes by hand. The codes are:

- the keys of `Codes` in `internal/registry/diag.go` (the `REG-*` and `REF-*` codes);
- the keys of `FactsCodes` in `internal/registry/facts.go`, and every other constant in the
  module's non-test Go files whose value is a `FACTS-*` code (`jpack terraform facts` declares
  one);
- the keys of `Codes` in `internal/emit/codes.go` (the `EMIT-*` codes, the host's and the
  conformance harness's);
- the constants `Codes()` in `internal/terraformfile/codes.go` returns (the `TF-*` codes);
- the keys of `Codes` in `internal/federation/codes.go`, the codes of registry format 1.1.0
  (proposed): the `FED-*` codes, one row each, and the `LOCK-*` codes, which are pending (below);
- every constant in the non-test Go files of a `cmd/jpack-emit-*` package whose value is an `x-`
  code, the emitter's own diagnostic codes. `cmd/jpack-emit-go-facts` declares none: its refusals
  are fixed lines, not diagnostics, so it has no rows.

`scripts/check-mutation-table.py` (the `mutation-table` check in `dev.json`) reads the codes from
those tables and fails when a code has no row, when a row's code no longer exists, when a row's
severity is not the one its table states, when a row's mutant site is not a function of a non-test
Go file, when a row's mutant is not `every A becomes B` with `A` the row's own code and in that
function's body (see the mutant column) and `B` neither blank nor `A`, when a row's provisional
column is not what the source marks, when `go test -list` does not list the
row's test in its package, or when no string literal in
that test names the row's subtest (as `go test` prints it), or when the test does not name the
row's code, as a literal or a constant holding it. For a subtest, a case that names any code of
the line must name the row's; `scripts/check-mutation-table-probe.py` holds that rule to a case
changed to expect another code, and holds the mutant and provisional rules to changed mutants,
headings and comments. The check does not
run the mutants; a reviewer does.

Format 1.1.0 is built a conformance class at a time. The codes of the classes not built yet (the
lock writer, the lock checker and the lock signature verifier, which raise the `LOCK-*` codes) are
in a section of `internal/federation/codes.go` whose heading says pending, and
`internal/federation/rows_test.go` lists those classes in `pendingClasses`, which `TestRows` holds
to the classes it runs: a listed class that is built, or a class neither listed nor run, fails it.
A pending code is in the code set and has no row. The check fails on a row for a pending code, on a
non-test Go file that names one in a string literal outside the table (a code raised before it has
a row), and on any pending code left once `pendingClasses` is empty. So the exemption ends with
the last class: building it empties the list, and the check then wants a row for every code that
was pending. The probes hold each of these.

## Columns

- **severity**: as the code's table states it. `TF-*`, `FACTS-ID-LIMIT` and the emitters' `x-`
  codes refuse the run and are errors.
- **package**: the Go package of the test.
- **test**: `TestX`, or `TestX/sub` as `go test` names a subtest (the `t.Run` name with each space
  written `_`). Run one row with `go test -count=1 -run '^TestX$/^sub$' ./package`, each part
  quoted as a regular expression.
- **mutant site**: the file and the function the mutant changes.
- **mutant**: `every A becomes B` means: in that function's body only, every occurrence of `A`
  (the row's code quoted, or a constant of the site's package that holds it) is replaced with
  `B`, another code of the same table, so the package still builds and the code reported is one
  the test does not accept. `A` is matched outside comments and as a whole token: a quoted code
  as a whole string literal, so `"EMIT-REQUEST"` is not part of `"EMIT-REQUEST-VERSION"`, and an
  identifier as a whole word.
- **provisional**: whether `VERSIONING.md` lists the code as this runtime's own and provisional.
  `n/a` marks an emitter's `x-` code, which `VERSIONING.md` places outside the registry line's promise.
  The check compares it with the source, which marks every code: the section heading of each
  code in `internal/emit/codes.go`, `internal/registry/diag.go` and `internal/registry/facts.go`,
  and the comment of a constant that holds a code (the `TF-*` codes, `FACTS-ID-LIMIT`). No
  section or comment says provisional: the registry companion's codes are released on
  `registry-v1.0.0`, the terraform-file companion's on `terraform-file-v1.0.0`, and the RFCs' are
  the RFCs', so every `REG-*`, `REF-*`, `FACTS-*` and `TF-*` row is `no`. The `FED-*` codes are
  the registry companion's 1.1.0 codes, proposed there and not this runtime's own, so they are `no`
  too.

| code | severity | package | test | mutant site | mutant | provisional |
|---|---|---|---|---|---|---|
| `REG-YAML-ENCODING` | error | `./internal/registry` | `TestImportRefusesOutsideTheSubset` | `internal/registry/yaml_parse.go:decodeYAMLText` | every `"REG-YAML-ENCODING"` becomes `"REG-YAML-DOCUMENTS"` | no |
| `REG-YAML-DOCUMENTS` | error | `./internal/registry` | `TestImportRefusesOutsideTheSubset` | `internal/registry/yaml_parse.go:parse` | every `"REG-YAML-DOCUMENTS"` becomes `"REG-YAML-DIRECTIVE"` | no |
| `REG-YAML-DIRECTIVE` | error | `./internal/registry` | `TestImportRefusesOutsideTheSubset` | `internal/registry/yaml_parse.go:parse` | every `"REG-YAML-DIRECTIVE"` becomes `"REG-YAML-TAG"` | no |
| `REG-YAML-TAG` | error | `./internal/registry` | `TestImportRefusesOutsideTheSubset` | `internal/registry/yaml_parse.go:skipProperties` | every `"REG-YAML-TAG"` becomes `"REG-YAML-DIRECTIVE"` | no |
| `REG-YAML-ANCHOR` | error | `./internal/registry` | `TestImportRefusesOutsideTheSubset` | `internal/registry/yaml_parse.go:skipProperties` | every `"REG-YAML-ANCHOR"` becomes `"REG-YAML-TAG"` | no |
| `REG-YAML-MERGE` | error | `./internal/registry` | `TestImportRefusesOutsideTheSubset` | `internal/registry/yaml_scalar.go:plainKeyCode` | every `"REG-YAML-MERGE"` becomes `"REG-YAML-SYNTAX"` | no |
| `REG-YAML-SCALAR` | error | `./internal/registry` | `TestImportRefusesOutsideTheSubset` | `internal/registry/yaml_scalar.go:resolvePlain` | every `"REG-YAML-SCALAR"` becomes `"REG-YAML-EMPTY"` | no |
| `REG-YAML-EMPTY` | error | `./internal/registry` | `TestImportRefusesOutsideTheSubset` | `internal/registry/yaml_parse.go:parseMappingValue` | every `"REG-YAML-EMPTY"` becomes `"REG-YAML-SYNTAX"` | no |
| `REG-YAML-KEY` | error | `./internal/registry` | `TestImportRefusesOutsideTheSubset` | `internal/registry/yaml_scalar.go:plainKeyCode` | every `"REG-YAML-KEY"` becomes `"REG-YAML-SYNTAX"` | no |
| `REG-YAML-SYNTAX` | error | `./internal/registry` | `TestImportRefusesOutsideTheSubset` | `internal/registry/yaml_parse.go:escape` | every `"REG-YAML-SYNTAX"` becomes `"REG-YAML-KEY"` | no |
| `REG-YAML-TRUNCATED` | error | `./internal/registry` | `TestImportStopsAfterTheRefusalLimit` | `internal/registry/yaml_parse.go:report` | every `"REG-YAML-TRUNCATED"` becomes `"REG-YAML-SYNTAX"` | no |
| `REG-REPORT-TRUNCATED` | warning | `./internal/registry` | `TestValidateReportIsCappedAtTheFirst1000` | `internal/registry/diag.go:sorted` | every `"REG-REPORT-TRUNCATED"` becomes `"REG-DOC-SIZE"` | no |
| `REG-DOC-SIZE` | error | `./internal/registry` | `TestAdmission/too_large` | `internal/registry/admit.go:admitValue` | every `"REG-DOC-SIZE"` becomes `"REG-DOC-DEPTH"` | no |
| `REG-DOC-DEPTH` | error | `./internal/registry` | `TestAdmission/too_deep` | `internal/registry/admit.go:admitValue` | every `"REG-DOC-DEPTH"` becomes `"REG-DOC-JSON"` | no |
| `REG-DOC-JSON` | error | `./internal/registry` | `TestAdmission/not_JSON` | `internal/registry/admit.go:admitValue` | every `"REG-DOC-JSON"` becomes `"REG-DOC-DUPLICATE"` | no |
| `REG-DOC-DUPLICATE` | error | `./internal/registry` | `TestAdmission/duplicate_member` | `internal/registry/admit.go:admitValue` | every `"REG-DOC-DUPLICATE"` becomes `"REG-DOC-STRING"` | no |
| `REG-DOC-STRING` | error | `./internal/registry` | `TestAdmission/lone_surrogate` | `internal/registry/admit.go:admitValue` | every `"REG-DOC-STRING"` becomes `"REG-DOC-NUMBER"` | no |
| `REG-DOC-NUMBER` | error | `./internal/registry` | `TestAdmission/integer_too_large` | `internal/registry/admit.go:admitValue` | every `"REG-DOC-NUMBER"` becomes `"REG-DOC-SELECTOR"` | no |
| `REG-DOC-SELECTOR` | error | `./internal/registry` | `TestAdmission/wrong_selector` | `internal/registry/admit.go:admit` | every `"REG-DOC-SELECTOR"` becomes `"REG-DOC-JSON"` | no |
| `REG-KIND-UNKNOWN-MEMBER` | error | `./internal/registry` | `TestEachRuleReportsItsCode/kind_member_unknown` | `internal/registry/kind.go:readKind` | every `"REG-KIND-UNKNOWN-MEMBER"` becomes `"REG-KIND-FORM"` | no |
| `REG-KIND-MISSING-MEMBER` | error | `./internal/registry` | `TestEveryKindDiagnosticCarriesTheKind` | `internal/registry/kind.go:readKind` | every `"REG-KIND-MISSING-MEMBER"` becomes `"REG-KIND-FORM"` | no |
| `REG-KIND-FORM` | error | `./internal/registry` | `TestEveryKindDiagnosticCarriesTheKind` | `internal/registry/kind.go:readKind` | every `"REG-KIND-FORM"` becomes `"REG-KIND-MISSING-MEMBER"` | no |
| `REG-KIND-SCHEMA-KEYWORD` | error | `./internal/registry` | `TestEachRuleReportsItsCode/kind_schema_oneOf` | `internal/registry/schema.go:compile` | every `"REG-KIND-SCHEMA-KEYWORD"` becomes `"REG-KIND-SCHEMA-FORM"` | no |
| `REG-KIND-SCHEMA-FORM` | error | `./internal/registry` | `TestOtherDistinctListsAreCheckedInOnePass` | `internal/registry/schema.go:distinctStrings` | every `"REG-KIND-SCHEMA-FORM"` becomes `"REG-KIND-SCHEMA-KEYWORD"` | no |
| `REG-KIND-SCHEMA-LIMIT` | error | `./internal/registry` | `TestSchemaLimits/17_branches` | `internal/registry/schema.go:branches` | every `"REG-KIND-SCHEMA-LIMIT"` becomes `"REG-KIND-SCHEMA-FORM"` | no |
| `REG-KIND-SCHEMA-REF` | error | `./internal/registry` | `TestEachRuleReportsItsCode/kind_$ref_to_nothing` | `internal/registry/schema.go:resolveRefs` | every `"REG-KIND-SCHEMA-REF"` becomes `"REG-KIND-SCHEMA-FORM"` | no |
| `REG-KIND-PATTERN` | error | `./internal/registry` | `TestEachRuleReportsItsCode/kind_schema_pattern_with_look-ahead` | `internal/registry/pattern.go:compile` | every `"REG-KIND-PATTERN"` becomes `"REG-KIND-PATTERN-LIMIT"` | no |
| `REG-KIND-PATTERN-LIMIT` | error | `./internal/registry` | `TestAPatternsSizeIsCountedAsWritten` | `internal/registry/pattern.go:compile` | every `"REG-KIND-PATTERN-LIMIT"` becomes `"REG-KIND-PATTERN"` | no |
| `REG-KIND-POINTER-FORM` | error | `./internal/registry` | `TestEachRuleReportsItsCode/kind_secrets_pattern_not_a_pointer` | `internal/registry/kind.go:readPointerPatterns` | every `"REG-KIND-POINTER-FORM"` becomes `"REG-KIND-POINTER-UNREACHABLE"` | no |
| `REG-KIND-POINTER-UNREACHABLE` | error | `./internal/registry` | `TestEachRuleReportsItsCode/kind_pointer_pattern_unreachable` | `internal/registry/kind.go:readPointerPatterns` | every `"REG-KIND-POINTER-UNREACHABLE"` becomes `"REG-KIND-POINTER-FORM"` | no |
| `REG-KIND-POINTER-CONFLICT` | error | `./internal/registry` | `TestEachRuleReportsItsCode/kind_position_both_secret_and_reference` | `internal/registry/kind.go:readKind` | every `"REG-KIND-POINTER-CONFLICT"` becomes `"REG-KIND-FORM"` | no |
| `REG-KIND-DEPRECATED` | warning | `./internal/registry` | `TestEachRuleReportsItsCode/deprecated_kind` | `internal/registry/registry.go:ValidateDocuments` | every `"REG-KIND-DEPRECATED"` becomes `"REG-PIN-UNPINNED"` | no |
| `REG-REGISTRY-UNKNOWN-MEMBER` | error | `./internal/registry` | `TestEachRuleReportsItsCode/unknown_registry_member` | `internal/registry/shape.go:readRegistry` | every `"REG-REGISTRY-UNKNOWN-MEMBER"` becomes `"REG-REGISTRY-FORM"` | no |
| `REG-REGISTRY-MISSING-MEMBER` | error | `./internal/registry` | `TestEachRuleReportsItsCode/missing_owners` | `internal/registry/shape.go:readRegistry` | every `"REG-REGISTRY-MISSING-MEMBER"` becomes `"REG-REGISTRY-FORM"` | no |
| `REG-REGISTRY-FORM` | error | `./internal/registry` | `TestEachRuleReportsItsCode/bad_environment_name` | `internal/registry/shape.go:readNames` | every `"REG-REGISTRY-FORM"` becomes `"REG-REGISTRY-DUPLICATE"` | no |
| `REG-REGISTRY-DUPLICATE` | error | `./internal/registry` | `TestEachRuleReportsItsCode/duplicate_environment` | `internal/registry/shape.go:readNames` | every `"REG-REGISTRY-DUPLICATE"` becomes `"REG-REGISTRY-FORM"` | no |
| `REG-REGISTRY-ORDER` | error | `./internal/registry` | `TestEachRuleReportsItsCode/entries_out_of_order` | `internal/registry/shape.go:readRegistry` | every `"REG-REGISTRY-ORDER"` becomes `"REG-REGISTRY-FORM"` | no |
| `REF-SECRET-SCOPE-FORM` | error | `./internal/registry` | `TestEachRuleReportsItsCode/scope_missing_for_an_environment` | `internal/registry/shape.go:readScopes` | every `"REF-SECRET-SCOPE-FORM"` becomes `"REF-SECRET-SCOPE-SHARED"` | no |
| `REF-SECRET-SCOPE-SHARED` | error | `./internal/registry` | `TestEachRuleReportsItsCode/scope_shared_between_environments` | `internal/registry/shape.go:readScopes` | every `"REF-SECRET-SCOPE-SHARED"` becomes `"REF-SECRET-SCOPE-FORM"` | no |
| `REG-PIN-MISSING` | error | `./internal/registry` | `TestEachRuleReportsItsCode/pin_digest_missing` | `internal/registry/registry.go:ValidateDocuments` | every `"REG-PIN-MISSING"` becomes `"REG-PIN-MISMATCH"` | no |
| `REG-PIN-MISMATCH` | error | `./internal/registry` | `TestEachRuleReportsItsCode/pin_version_mismatch` | `internal/registry/registry.go:ValidateDocuments` | every `"REG-PIN-MISMATCH"` becomes `"REG-PIN-MISSING"` | no |
| `REG-PIN-UNPINNED` | warning | `./internal/registry` | `TestUnpinnedSuppliedKindWarns` | `internal/registry/registry.go:ValidateDocuments` | every `"REG-PIN-UNPINNED"` becomes `"REG-KIND-DEPRECATED"` | no |
| `REG-ENTRY-KIND` | error | `./internal/registry` | `TestEachRuleReportsItsCode/unpinned_entry_kind` | `internal/registry/registry.go:ValidateDocuments` | every `"REG-ENTRY-KIND"` becomes `"REG-ENTRY-ID"` | no |
| `REG-ENTRY-ID` | error | `./internal/registry` | `TestEachRuleReportsItsCode/entry_id_outside_its_pattern` | `internal/registry/registry.go:ValidateDocuments` | every `"REG-ENTRY-ID"` becomes `"REG-ENTRY-KIND"` | no |
| `REG-ENTRY-DUPLICATE` | error | `./internal/registry` | `TestOverlayPrefixTargetAndDuplicates` | `internal/registry/registry.go:ValidateDocuments` | every `"REG-ENTRY-DUPLICATE"` becomes `"REG-ENTRY-ID"` | no |
| `REG-ENTRY-OWNER` | error | `./internal/registry` | `TestEachRuleReportsItsCode/unknown_owner` | `internal/registry/registry.go:ValidateDocuments` | every `"REG-ENTRY-OWNER"` becomes `"REG-ENTRY-ENVIRONMENT"` | no |
| `REG-ENTRY-ENVIRONMENT` | error | `./internal/registry` | `TestEachRuleReportsItsCode/undeclared_entry_environment` | `internal/registry/registry.go:ValidateDocuments` | every `"REG-ENTRY-ENVIRONMENT"` becomes `"REG-ENTRY-OWNER"` | no |
| `REG-OVERLAY-ENVIRONMENT` | error | `./internal/registry` | `TestEachRuleReportsItsCode/overlay_for_undeclared_environment` | `internal/registry/shape.go:checkOverlays` | every `"REG-OVERLAY-ENVIRONMENT"` becomes `"REG-OVERLAY-DUPLICATE"` | no |
| `REG-OVERLAY-DUPLICATE` | error | `./internal/registry` | `TestEachRuleReportsItsCode/two_overlays_for_one_environment` | `internal/registry/shape.go:checkOverlays` | every `"REG-OVERLAY-DUPLICATE"` becomes `"REG-OVERLAY-ENVIRONMENT"` | no |
| `REG-OVERLAY-PATCH-DUPLICATE` | error | `./internal/registry` | `TestAFailedPatchSkipsTheEntryInThatEnvironment` | `internal/registry/shape.go:checkOverlays` | every `"REG-OVERLAY-PATCH-DUPLICATE"` becomes `"REG-OVERLAY-ENTRY-MISSING"` | no |
| `REG-OVERLAY-ENTRY-MISSING` | error | `./internal/registry` | `TestEachRuleReportsItsCode/patch_for_a_missing_entry` | `internal/registry/shape.go:checkOverlays` | every `"REG-OVERLAY-ENTRY-MISSING"` becomes `"REG-OVERLAY-ENTRY-ABSENT"` | no |
| `REG-OVERLAY-ENTRY-ABSENT` | error | `./internal/registry` | `TestEachRuleReportsItsCode/patch_for_an_entry_absent_in_the_environment` | `internal/registry/shape.go:checkOverlays` | every `"REG-OVERLAY-ENTRY-ABSENT"` becomes `"REG-OVERLAY-ENTRY-MISSING"` | no |
| `REG-OVERLAY-POINTER` | error | `./internal/registry` | `TestEachRuleReportsItsCode/malformed_patch_pointer` | `internal/registry/shape.go:checkOverlays` | every `"REG-OVERLAY-POINTER"` becomes `"REG-OVERLAY-NULL"` | no |
| `REG-OVERLAY-NOT-OVERLAYABLE` | error | `./internal/registry` | `TestEachRuleReportsItsCode/patch_on_a_member_that_is_not_overlayable` | `internal/registry/shape.go:checkOverlays` | every `"REG-OVERLAY-NOT-OVERLAYABLE"` becomes `"REG-OVERLAY-TARGET"` | no |
| `REG-OVERLAY-TARGET` | error | `./internal/registry` | `TestOverlayPrefixTargetAndDuplicates` | `internal/registry/shape.go:checkOverlays` | every `"REG-OVERLAY-TARGET"` becomes `"REG-OVERLAY-NOT-OVERLAYABLE"` | no |
| `REG-OVERLAY-NULL` | error | `./internal/registry` | `TestEachRuleReportsItsCode/patch_sets_null` | `internal/registry/shape.go:checkOverlays` | every `"REG-OVERLAY-NULL"` becomes `"REG-OVERLAY-POINTER"` | no |
| `REG-OVERLAY-PREFIX` | error | `./internal/registry` | `TestOverlayPrefixTargetAndDuplicates` | `internal/registry/shape.go:checkOverlays` | every `"REG-OVERLAY-PREFIX"` becomes `"REG-OVERLAY-TARGET"` | no |
| `REG-VALUE-TYPE` | error | `./internal/registry` | `TestEachRuleReportsItsCode/wrong_type` | `internal/registry/schema_validate.go:validateOwn` | every `"REG-VALUE-TYPE"` becomes `"REG-VALUE-ENUM"` | no |
| `REG-VALUE-ENUM` | error | `./internal/registry` | `TestEachRuleReportsItsCode/not_in_enum` | `internal/registry/schema_validate.go:validateOwn` | every `"REG-VALUE-ENUM"` becomes `"REG-VALUE-CONST"` | no |
| `REG-VALUE-CONST` | error | `./internal/registry` | `TestEveryValueCode` | `internal/registry/schema_validate.go:validateOwn` | every `"REG-VALUE-CONST"` becomes `"REG-VALUE-ENUM"` | no |
| `REG-VALUE-REQUIRED` | error | `./internal/registry` | `TestEachRuleReportsItsCode/required_member_absent` | `internal/registry/schema_validate.go:validateObject` | every `"REG-VALUE-REQUIRED"` becomes `"REG-VALUE-ADDITIONAL"` | no |
| `REG-VALUE-ADDITIONAL` | error | `./internal/registry` | `TestEachRuleReportsItsCode/additional_member` | `internal/registry/schema_validate.go:validateObject` | every `"REG-VALUE-ADDITIONAL"` becomes `"REG-VALUE-REQUIRED"` | no |
| `REG-VALUE-PROPERTY-COUNT` | error | `./internal/registry` | `TestEveryValueCode` | `internal/registry/schema_validate.go:validateObject` | every `"REG-VALUE-PROPERTY-COUNT"` becomes `"REG-VALUE-PROPERTY-NAME"` | no |
| `REG-VALUE-PROPERTY-NAME` | error | `./internal/registry` | `TestEveryValueCode` | `internal/registry/schema_validate.go:validateObject` | every `"REG-VALUE-PROPERTY-NAME"` becomes `"REG-VALUE-PROPERTY-COUNT"` | no |
| `REG-VALUE-ITEM-COUNT` | error | `./internal/registry` | `TestEveryValueCode` | `internal/registry/schema_validate.go:validateOwn` | every `"REG-VALUE-ITEM-COUNT"` becomes `"REG-VALUE-LENGTH"` | no |
| `REG-VALUE-UNIQUE` | error | `./internal/registry` | `TestEachRuleReportsItsCode/items_not_unique` | `internal/registry/schema_validate.go:validateOwn` | every `"REG-VALUE-UNIQUE"` becomes `"REG-VALUE-ITEM-COUNT"` | no |
| `REG-VALUE-LENGTH` | error | `./internal/registry` | `TestEveryValueCode` | `internal/registry/schema_validate.go:validateOwn` | every `"REG-VALUE-LENGTH"` becomes `"REG-VALUE-ITEM-COUNT"` | no |
| `REG-VALUE-PATTERN` | error | `./internal/registry` | `TestEveryValueCode` | `internal/registry/schema_validate.go:validateOwn` | every `"REG-VALUE-PATTERN"` becomes `"REG-VALUE-LENGTH"` | no |
| `REG-VALUE-RANGE` | error | `./internal/registry` | `TestEachRuleReportsItsCode/overlay_breaks_the_schema_in_one_environment` | `internal/registry/schema_validate.go:validateOwn` | every `"REG-VALUE-RANGE"` becomes `"REG-VALUE-TYPE"` | no |
| `REG-VALUE-ANY-OF` | error | `./internal/registry` | `TestEveryValueCode` | `internal/registry/schema_validate.go:validate` | every `"REG-VALUE-ANY-OF"` becomes `"REG-VALUE-NOT"` | no |
| `REG-VALUE-NOT` | error | `./internal/registry` | `TestEveryValueCode` | `internal/registry/schema_validate.go:validateOwn` | every `"REG-VALUE-NOT"` becomes `"REG-VALUE-ANY-OF"` | no |
| `REG-VALUE-DEPRECATED` | warning | `./internal/registry` | `TestEachRuleReportsItsCode/deprecated_member` | `internal/registry/schema_validate.go:validateOwn` | every `"REG-VALUE-DEPRECATED"` becomes `"REG-VALUE-TYPE"` | no |
| `REF-ENTRY-FORM` | error | `./internal/registry` | `TestEachRuleReportsItsCode/entry_reference_malformed` | `internal/registry/registry.go:checkResolvedEntry` | every `"REF-ENTRY-FORM"` becomes `"REF-ENTRY-MISSING"` | no |
| `REF-ENTRY-KIND` | error | `./internal/registry` | `TestEachRuleReportsItsCode/entry_reference_to_a_kind_the_position_does_not_allow` | `internal/registry/registry.go:checkResolvedEntry` | every `"REF-ENTRY-KIND"` becomes `"REF-ENTRY-MISSING"` | no |
| `REF-ENTRY-MISSING` | error | `./internal/registry` | `TestEachRuleReportsItsCode/entry_reference_to_a_missing_entry` | `internal/registry/registry.go:checkResolvedEntry` | every `"REF-ENTRY-MISSING"` becomes `"REF-ENTRY-KIND"` | no |
| `REF-ENTRY-ABSENT-IN-ENVIRONMENT` | error | `./internal/registry` | `TestEachRuleReportsItsCode/entry_reference_absent_in_an_environment` | `internal/registry/registry.go:checkResolvedEntry` | every `"REF-ENTRY-ABSENT-IN-ENVIRONMENT"` becomes `"REF-ENTRY-MISSING"` | no |
| `REF-ENTRY-POINTER` | error | `./internal/registry` | `TestEachRuleReportsItsCode/entry_reference_pointer_addresses_nothing` | `internal/registry/registry.go:checkResolvedEntry` | every `"REF-ENTRY-POINTER"` becomes `"REF-ENTRY-MISSING"` | no |
| `REF-SECRET-FORM` | error | `./internal/registry` | `TestEachRuleReportsItsCode/secret_written_as_a_literal` | `internal/registry/refs.go:parseSecretReference` | every `"REF-SECRET-FORM"` becomes `"REF-SECRET-PIN"` | no |
| `REF-SECRET-STORE` | error | `./internal/registry` | `TestEachRuleReportsItsCode/secret_in_an_unknown_store` | `internal/registry/refs.go:parseSecretReference` | every `"REF-SECRET-STORE"` becomes `"REF-SECRET-FORM"` | no |
| `REF-SECRET-PIN` | error | `./internal/registry` | `TestEachRuleReportsItsCode/secret_not_pinned` | `internal/registry/refs.go:parseSecretReference` | every `"REF-SECRET-PIN"` becomes `"REF-SECRET-FORM"` | no |
| `REF-SECRET-SCOPE` | error | `./internal/registry` | `TestEachRuleReportsItsCode/production_secret_from_a_development_project` | `internal/registry/registry.go:checkResolvedEntry` | every `"REF-SECRET-SCOPE"` becomes `"REF-SECRET-UNSCOPED"` | no |
| `REF-SECRET-UNSCOPED` | warning | `./internal/registry` | `TestEachRuleReportsItsCode/no_scopes_declared` | `internal/registry/registry.go:checkResolvedEntry` | every `"REF-SECRET-UNSCOPED"` becomes `"REF-SECRET-SCOPE"` | no |
| `REG-BOUND-ENTRIES` | error | `./internal/registry` | `TestEachRuleReportsItsCode/kind's_maxEntries_exceeded` | `internal/registry/registry.go:ValidateDocuments` | every `"REG-BOUND-ENTRIES"` becomes `"REG-BOUND-ENTRY-BYTES"` | no |
| `REG-BOUND-ENTRY-BYTES` | error | `./internal/registry` | `TestEachRuleReportsItsCode/kind's_maxEntryBytes_exceeded` | `internal/registry/registry.go:ValidateDocuments` | every `"REG-BOUND-ENTRY-BYTES"` becomes `"REG-BOUND-ENTRIES"` | no |
| `REG-BOUND-VALIDATION-WORK` | error | `./internal/registry` | `TestValidationIsBoundedOverTheRegistry` | `internal/registry/registry.go:checkResolvedEntry` | every `"REG-BOUND-VALIDATION-WORK"` becomes `"REG-BOUND-ENTRY-BYTES"` | no |
| `FACTS-POINTER-UNKNOWN` | warning | `./internal/registry` | `TestCheckPackAdvisories` | `internal/registry/facts_check.go:checkCondition` | every `"FACTS-POINTER-UNKNOWN"` becomes `"FACTS-TYPE-ORDERED"` | no |
| `FACTS-TYPE-ORDERED` | warning | `./internal/registry` | `TestCheckPackAdvisories` | `internal/registry/facts_check.go:checkCondition` | every `"FACTS-TYPE-ORDERED"` becomes `"FACTS-VALUE-UNREACHABLE"` | no |
| `FACTS-VALUE-UNREACHABLE` | warning | `./internal/registry` | `TestCheckPackAdvisories` | `internal/registry/facts_check.go:checkCondition` | every `"FACTS-VALUE-UNREACHABLE"` becomes `"FACTS-POINTER-UNKNOWN"` | no |
| `FACTS-DOCUMENT-LIMIT` | error | `./internal/registry` | `TestFactsDocumentOverTheLimitIsReportedNotWritten` | `internal/registry/facts.go:WriteFacts` | every `CodeFactsDocumentLimit` becomes `"FACTS-CONTRACT-LIMIT"` | no |
| `FACTS-CONTRACT-LIMIT` | error | `./internal/registry` | `TestFactsContractOverTheWorkLimit` | `internal/registry/facts_contract.go:FactsContract` | every `"FACTS-CONTRACT-LIMIT"` becomes `"FACTS-CONTRACT-FORM"` | no |
| `FACTS-CONTRACT-FORM` | error | `./internal/registry` | `TestFactsContractRefusals` | `internal/registry/facts_check.go:ReadFactsContract` | every `"FACTS-CONTRACT-FORM"` becomes `"FACTS-CONTRACT-ORDER"` | no |
| `FACTS-CONTRACT-ORDER` | error | `./internal/registry` | `TestFactsContractRefusals` | `internal/registry/facts_check.go:ReadFactsContract` | every `"FACTS-CONTRACT-ORDER"` becomes `"FACTS-CONTRACT-FORM"` | no |
| `FACTS-CHECK-LIMIT` | error | `./internal/registry` | `TestCheckPackWorkIsBoundedOverOneCheck` | `internal/registry/facts_check.go:CheckPack` | every `"FACTS-CHECK-LIMIT"` becomes `"FACTS-CONTRACT-LIMIT"` | no |
| `FACTS-ID-LIMIT` | error | `./internal/cli` | `TestTerraformFactsHoldsTheBatchIDLimit` | `internal/cli/terraform_facts.go:terraformFactsCheck` | every `CodeFactsIDLimit` becomes `registry.CodeFactsDocumentLimit` | no |
| `EMIT-REQUEST-VERSION` | error | `./internal/emit` | `TestEmitChecksOptionsAndRequestVersions` | `internal/emit/request.go:FormRequests` | every `"EMIT-REQUEST-VERSION"` becomes `"EMIT-KIND-RANGE"` | no |
| `EMIT-KIND-RANGE` | error | `./internal/emit` | `TestEmitRefusesAKindPinOutsideTheManifestsReads/version_out_of_range` | `internal/emit/request.go:FormRequests` | every `"EMIT-KIND-RANGE"` becomes `"EMIT-KIND-ID"` | no |
| `EMIT-REQUEST` | error | `./internal/emit` | `TestFormRequestsBoundsTheRequestBytes` | `internal/emit/request.go:FormRequests` | every `"EMIT-REQUEST"` becomes `"EMIT-OPTIONS"` | no |
| `EMIT-PROCESS` | error | `./internal/emit` | `TestEmitReportsAnEmitterThatFails/non-zero_exit` | `internal/emit/run.go:Run` | every `"EMIT-PROCESS"` becomes `"EMIT-RESPONSE"` | no |
| `EMIT-RESPONSE` | error | `./internal/emit` | `TestResponseRefusesEachMalformedPart` | `internal/emit/response.go:ParseResponse` | every `"EMIT-RESPONSE"` becomes `"EMIT-MANIFEST"` | no |
| `EMIT-DEPRECATED` | warning | `./internal/emit` | `TestEmitReportsAnInvalidRegistryAndADeprecatedEmitter` | `internal/emit/host.go:Emit` | every `"EMIT-DEPRECATED"` becomes `"EMIT-ENV-UNSET"` | no |
| `EMIT-STRAY-WRITE` | error | `./internal/emit/conformance` | `TestStrayWrites/change` | `internal/emit/conformance/stray.go:diffTree` | every `"EMIT-STRAY-WRITE"` becomes `"EMIT-VECTOR"` | no |
| `EMIT-MANIFEST` | error | `./internal/emit` | `TestManifestRefusesEachMalformedMember` | `internal/emit/manifest.go:ReadManifest` | every `"EMIT-MANIFEST"` becomes `"EMIT-OPTIONS"` | no |
| `EMIT-OWNS-OVERLAP` | error | `./internal/emit` | `TestEmitRefusesOwnsThatOverlapAnotherManifests` | `internal/emit/host.go:ownsOverlaps` | every `"EMIT-OWNS-OVERLAP"` becomes `"EMIT-MANIFEST"` | no |
| `EMIT-KIND-ID` | error | `./internal/emit` | `TestEmitRefusesAKindPinOutsideTheManifestsReads/another_kind_id` | `internal/emit/request.go:FormRequests` | every `"EMIT-KIND-ID"` becomes `"EMIT-KIND-RANGE"` | no |
| `EMIT-OPTIONS` | error | `./internal/emit` | `TestEmitChecksOptionsAndRequestVersions` | `internal/emit/request.go:FormRequests` | every `"EMIT-OPTIONS"` becomes `"EMIT-REQUEST"` | no |
| `EMIT-ENV-UNSET` | warning | `./internal/emit` | `TestEmitGivesTheEmitterOnlyItsEnvironment` | `internal/emit/run.go:Run` | every `"EMIT-ENV-UNSET"` becomes `"EMIT-PROCESS"` | no |
| `EMIT-LINK` | error | `./internal/emit` | `TestEmitRefusesALinkAtOrAboveAnOwnedPath` | `internal/emit/files.go:scanOwned` | every `"EMIT-LINK"` becomes `"EMIT-WRITE"` | no |
| `EMIT-PATH-UNREADABLE` | error | `./internal/emit/conformance` | `TestStrayWriteRowStaysWhenTheCopyCannotBeMadeAgain` | `internal/emit/conformance/stray.go:fresh` | every `"EMIT-PATH-UNREADABLE"` becomes `"EMIT-HARNESS-BOUND"` | no |
| `EMIT-WRITE` | error | `./internal/emit` | `TestEmitDeletesOnlyOwnedFilesAndStopsAtAnUnownedBlocker` | `internal/emit/files.go:write` | every `"EMIT-WRITE"` becomes `"EMIT-LINK"` | no |
| `EMIT-REPORT-TRUNCATED` | warning | `./internal/emit` | `TestEmitHoldsTheReportedDiagnosticsToTheirBound` | `internal/emit/host.go:finish` | every `"EMIT-REPORT-TRUNCATED"` becomes `"EMIT-ENV-UNSET"` | no |
| `EMIT-INTERRUPTED` | error | `./internal/emit` | `TestARunInterruptedBeforeItsWriteWritesNothing` | `internal/emit/host.go:Emit` | every `"EMIT-INTERRUPTED"` becomes `"EMIT-PROCESS"` | no |
| `EMIT-STAGING-LEFTOVER` | warning | `./internal/emit` | `TestEmitReportsStagingLeftoversItDoesNotOwn` | `internal/emit/host.go:Emit` | every `"EMIT-STAGING-LEFTOVER"` becomes `"EMIT-REPORT-TRUNCATED"` | no |
| `EMIT-VECTOR` | error | `./internal/emit/conformance` | `TestVectorLayoutRefusals` | `internal/emit/conformance/vectors.go:layoutVector` | every `"EMIT-VECTOR"` becomes `"EMIT-PATH-UNREADABLE"` | no |
| `EMIT-HARNESS-BOUND` | error | `./internal/emit/conformance` | `TestReferencesBound` | `internal/emit/conformance/references.go:references` | every `"EMIT-HARNESS-BOUND"` becomes `"EMIT-VECTOR"` | no |
| `EMIT-LINK-OMITTED` | warning | `./internal/emit/conformance` | `TestALinkOutOfTheRepositoryIsLeftOut` | `internal/emit/conformance/harness.go:Run` | every `"EMIT-LINK-OMITTED"` becomes `"EMIT-HARNESS-BOUND"` | no |
| `TF-SIZE` | error | `./internal/terraformfile` | `TestEveryRefusalCodeIsReported` | `internal/terraformfile/read.go:read` | every `CodeSize` becomes `CodeJSON` | no |
| `TF-JSON` | error | `./internal/terraformfile` | `TestEveryRefusalCodeIsReported` | `internal/terraformfile/read.go:read` | every `CodeJSON` becomes `CodeFormat` | no |
| `TF-FORMAT` | error | `./internal/terraformfile` | `TestEveryRefusalCodeIsReported` | `internal/terraformfile/read.go:read` | every `CodeFormat` becomes `CodeDocument` | no |
| `TF-DOCUMENT` | error | `./internal/terraformfile` | `TestEveryRefusalCodeIsReported` | `internal/terraformfile/read.go:read` | every `CodeDocument` becomes `CodeSensitivity` | no |
| `TF-SENSITIVITY` | error | `./internal/terraformfile` | `TestEveryRefusalCodeIsReported` | `internal/terraformfile/read.go:read` | every `CodeSensitivity` becomes `CodeSelectBound` | no |
| `TF-SELECT-BOUND` | error | `./internal/terraformfile` | `TestEveryRefusalCodeIsReported` | `internal/terraformfile/read.go:read` | every `CodeSelectBound` becomes `CodeNumber` | no |
| `TF-NUMBER` | error | `./internal/terraformfile` | `TestEveryRefusalCodeIsReported` | `internal/terraformfile/read.go:read` | every `CodeNumber` becomes `CodeValueLimit` | no |
| `TF-VALUE-LIMIT` | error | `./internal/terraformfile` | `TestTheValueLimitHoldsAtItsBoundary` | `internal/terraformfile/read.go:read` | every `CodeValueLimit` becomes `CodeNumber` | no |
| `x-go-loader-schema` | error | `./cmd/jpack-emit-go-loader` | `TestFindings` | `cmd/jpack-emit-go-loader/schema.go:objectType` | every `findSchema` becomes `findSecret` | n/a |
| `x-go-loader-secret` | error | `./cmd/jpack-emit-go-loader` | `TestFindings` | `cmd/jpack-emit-go-loader/schema.go:markSecret` | every `findSecret` becomes `findSchema` | n/a |
| `x-go-loader-name` | error | `./cmd/jpack-emit-go-loader` | `TestFindings` | `cmd/jpack-emit-go-loader/names.go:namePosition` | every `findName` becomes `findCollide` | n/a |
| `x-go-loader-collide` | error | `./cmd/jpack-emit-go-loader` | `TestFindings` | `cmd/jpack-emit-go-loader/names.go:claim` | every `findCollide` becomes `findName` | n/a |
| `x-ts-declaration-missing` | error | `./cmd/jpack-emit-ts-constants` | `TestDeclarationsAreCheckedAgainstTheirPins` | `cmd/jpack-emit-ts-constants/declarations.go:bindDeclarations` | every `codeDeclarationMissing` becomes `codeDeclarationForm` | n/a |
| `x-ts-declaration-mismatch` | error | `./cmd/jpack-emit-ts-constants` | `TestDeclarationsAreCheckedAgainstTheirPins` | `cmd/jpack-emit-ts-constants/declarations.go:bindDeclarations` | every `codeDeclarationMismatch` becomes `codeDeclarationForm` | n/a |
| `x-ts-declaration-unpinned` | error | `./cmd/jpack-emit-ts-constants` | `TestDeclarationsAreCheckedAgainstTheirPins` | `cmd/jpack-emit-ts-constants/declarations.go:bindDeclarations` | every `codeDeclarationUnpinned` becomes `codeDeclarationForm` | n/a |
| `x-ts-declaration-form` | error | `./cmd/jpack-emit-ts-constants` | `TestDeclarationsAreCheckedAgainstTheirPins` | `cmd/jpack-emit-ts-constants/declarations.go:bindDeclarations` | every `codeDeclarationForm` becomes `codeDeclarationMismatch` | n/a |
| `x-ts-schema-unsupported` | error | `./cmd/jpack-emit-ts-constants` | `TestSchemaRefusals` | `cmd/jpack-emit-ts-constants/schema.go:schema` | every `codeSchemaUnsupported` becomes `codeSecretLiteral` | n/a |
| `x-ts-secret-in-array` | error | `./cmd/jpack-emit-ts-constants` | `TestSchemaRefusals` | `cmd/jpack-emit-ts-constants/schema.go:array` | every `codeSecretInArray` becomes `codeSchemaUnsupported` | n/a |
| `x-ts-secret-literal` | error | `./cmd/jpack-emit-ts-constants` | `TestSchemaRefusals` | `cmd/jpack-emit-ts-constants/schema.go:literals` | every `codeSecretLiteral` becomes `codeMemberProto` | n/a |
| `x-ts-name-reserved` | error | `./cmd/jpack-emit-ts-constants` | `TestNamesAreRefusedNeverRenamed` | `cmd/jpack-emit-ts-constants/names.go:checkNames` | every `codeNameReserved` becomes `codeNameCollision` | n/a |
| `x-ts-name-collision` | error | `./cmd/jpack-emit-ts-constants` | `TestNamesAreRefusedNeverRenamed` | `cmd/jpack-emit-ts-constants/names.go:checkNames` | every `codeNameCollision` becomes `codeNameReserved` | n/a |
| `x-ts-member-proto` | error | `./cmd/jpack-emit-ts-constants` | `TestSchemaRefusals` | `cmd/jpack-emit-ts-constants/schema.go:object` | every `codeMemberProto` becomes `codeSchemaUnsupported` | n/a |
| `x-ts-bounds` | error | `./cmd/jpack-emit-ts-constants` | `TestSharedTypesAreWrittenWithinTheBound` | `cmd/jpack-emit-ts-constants/write.go:generate` | every `codeBounds` becomes `codePath` | n/a |
| `x-ts-path` | error | `./cmd/jpack-emit-ts-constants` | `TestDeviceNameEnvironmentIsRefused` | `cmd/jpack-emit-ts-constants/write.go:generate` | every `codePath` becomes `codeBounds` | n/a |
| `FED-MANIFEST` | error | `./internal/federation` | `TestEveryCodeIsRaisedByARow/FED-MANIFEST` | `internal/federation/manifest.go:readManifest` | every `"FED-MANIFEST"` becomes `"FED-REPOSITORY"` | no |
| `FED-COMMIT` | error | `./internal/federation` | `TestEveryCodeIsRaisedByARow/FED-COMMIT` | `internal/federation/assemble.go:step1` | every `"FED-COMMIT"` becomes `"FED-REPOSITORY"` | no |
| `FED-REPOSITORY` | error | `./internal/federation` | `TestEveryCodeIsRaisedByARow/FED-REPOSITORY` | `internal/federation/assemble.go:step1` | every `"FED-REPOSITORY"` becomes `"FED-COMMIT"` | no |
| `FED-SOURCE-MISSING` | error | `./internal/federation` | `TestEveryCodeIsRaisedByARow/FED-SOURCE-MISSING` | `internal/federation/assemble.go:step1` | every `"FED-SOURCE-MISSING"` becomes `"FED-MANIFEST"` | no |
| `FED-ENVIRONMENTS` | error | `./internal/federation` | `TestEveryCodeIsRaisedByARow/FED-ENVIRONMENTS` | `internal/federation/assemble.go:step3` | every `"FED-ENVIRONMENTS"` becomes `"FED-REGISTRY-DUPLICATE"` | no |
| `FED-ENVIRONMENT-GAP` | error | `./internal/federation` | `TestEveryCodeIsRaisedByARow/FED-ENVIRONMENT-GAP` | `internal/federation/assemble.go:gaps` | every `"FED-ENVIRONMENT-GAP"` becomes `"FED-ENVIRONMENTS"` | no |
| `FED-KNOWN-LIMIT` | error | `./internal/federation` | `TestEveryCodeIsRaisedByARow/FED-KNOWN-LIMIT` | `internal/federation/assemble.go:run` | every `"FED-KNOWN-LIMIT"` becomes `"FED-SOURCE-ASSEMBLED"` | no |
| `FED-SOURCE-ASSEMBLED` | error | `./internal/federation` | `TestEveryCodeIsRaisedByARow/FED-SOURCE-ASSEMBLED` | `internal/federation/assemble.go:step3` | every `"FED-SOURCE-ASSEMBLED"` becomes `"FED-REGISTRY-DUPLICATE"` | no |
| `FED-REGISTRY-DUPLICATE` | error | `./internal/federation` | `TestEveryCodeIsRaisedByARow/FED-REGISTRY-DUPLICATE` | `internal/federation/assemble.go:step3` | every `"FED-REGISTRY-DUPLICATE"` becomes `"FED-SOURCE-ASSEMBLED"` | no |
| `FED-KIND-NOT-STANDARD` | error | `./internal/federation` | `TestEveryCodeIsRaisedByARow/FED-KIND-NOT-STANDARD` | `internal/federation/assemble.go:step4` | every `"FED-KIND-NOT-STANDARD"` becomes `"FED-KIND-CONFLICT"` | no |
| `FED-KIND-CONFLICT` | error | `./internal/federation` | `TestEveryCodeIsRaisedByARow/FED-KIND-CONFLICT` | `internal/federation/assemble.go:step4` | every `"FED-KIND-CONFLICT"` becomes `"FED-KIND-NOT-STANDARD"` | no |
| `FED-SCOPE-WIDENS` | error | `./internal/federation` | `TestEveryCodeIsRaisedByARow/FED-SCOPE-WIDENS` | `internal/federation/assemble.go:scopeWidenings` | every `"FED-SCOPE-WIDENS"` becomes `"FED-BOUND-WIDENS"` | no |
| `FED-BOUND-FORM` | error | `./internal/federation` | `TestEveryCodeIsRaisedByARow/FED-BOUND-FORM` | `internal/federation/bounds.go:readBounds` | every `"FED-BOUND-FORM"` becomes `"FED-BOUND-ENVIRONMENT"` | no |
| `FED-BOUND-ENVIRONMENT` | error | `./internal/federation` | `TestEveryCodeIsRaisedByARow/FED-BOUND-ENVIRONMENT` | `internal/federation/bounds.go:readBounds` | every `"FED-BOUND-ENVIRONMENT"` becomes `"FED-BOUND-FORM"` | no |
| `FED-BOUND-KIND` | error | `./internal/federation` | `TestEveryCodeIsRaisedByARow/FED-BOUND-KIND` | `internal/federation/boundscheck.go:step7` | every `"FED-BOUND-KIND"` becomes `"FED-BOUND-POINTER-UNREACHABLE"` | no |
| `FED-BOUND-POINTER-UNREACHABLE` | error | `./internal/federation` | `TestEveryCodeIsRaisedByARow/FED-BOUND-POINTER-UNREACHABLE` | `internal/federation/boundscheck.go:step7` | every `"FED-BOUND-POINTER-UNREACHABLE"` becomes `"FED-BOUND-KIND"` | no |
| `FED-BOUND-NOT-COMPARABLE` | error | `./internal/federation` | `TestEveryCodeIsRaisedByARow/FED-BOUND-NOT-COMPARABLE` | `internal/federation/bounds.go:readBound` | every `"FED-BOUND-NOT-COMPARABLE"` becomes `"FED-BOUND-FORM"` | no |
| `FED-BOUND-OVERLAP` | error | `./internal/federation` | `TestEveryCodeIsRaisedByARow/FED-BOUND-OVERLAP` | `internal/federation/boundscheck.go:compare` | every `"FED-BOUND-OVERLAP"` becomes `"FED-BOUND-WIDENS"` | no |
| `FED-BOUND-WIDENS` | error | `./internal/federation` | `TestEveryCodeIsRaisedByARow/FED-BOUND-WIDENS` | `internal/federation/boundscheck.go:step7` | every `"FED-BOUND-WIDENS"` becomes `"FED-BOUND-OVERLAP"` | no |
| `FED-BOUND-ENTRIES` | error | `./internal/federation` | `TestEveryCodeIsRaisedByARow/FED-BOUND-ENTRIES` | `internal/federation/boundscheck.go:entryCounts` | every `"FED-BOUND-ENTRIES"` becomes `"FED-VALUE-BOUND"` | no |
| `FED-VALUE-BOUND` | error | `./internal/federation` | `TestEveryCodeIsRaisedByARow/FED-VALUE-BOUND` | `internal/federation/boundscheck.go:values` | every `"FED-VALUE-BOUND"` becomes `"FED-BOUND-ENTRIES"` | no |
| `FED-INDEX-MISMATCH` | error | `./internal/federation` | `TestEveryCodeIsRaisedByARow/FED-INDEX-MISMATCH` | `internal/federation/index.go:CheckIndex` | every `"FED-INDEX-MISMATCH"` becomes `"FED-MANIFEST"` | no |

## Start-up module

The start-up module (`startup/`, a nested module, ADR-0096) reports no codes. Its refusals are
its exported error values and its limits its exported constants, under the module's 1.0 promise,
so they are the identifiers of this table's rows. Each function of a non-test Go file of
`startup/` that uses one has one row: the test that holds that refusal or limit there, and a
recorded mutant of that function. The rows follow from the source:

- every exported variable of `startup/`, each an `errors.New` value (the check refuses to read
  an exported variable of another form);
- every exported constant whose value is not a string literal (`Redacted` is one, and is not a
  limit);
- for each of them, every function of a non-test Go file that uses it outside its comments and
  string literals, whatever the file's build constraint.

`scripts/check-mutation-table.py` fails when such a function has no row or two, when a row's
identifier is not one of them or its function does not use it, when a row's mutant does not
apply (below), when the row's test does not name the identifier outside its comments, when the
test is not declared in a test file of the module on `linux`, `darwin` or `windows` (`go list`),
and when a test in a file the host builds is not listed by `go test -list`. The probe holds these
rules to rows, sources and mutants changed in memory.

Some of what the module does is not a refusal or a limit with an identifier, and has no row: the
trim of exactly one trailing `"\n"` or `"\r\n"` (`TestReadEnvTrimsExactlyOneTrailingNewline`,
`TestReadFileTrimsExactlyOneTrailingNewline`), the open that does not wait on a named pipe
(`TestReadFileOpensWithoutWaitingOnANamedPipe`), and the errors of the operating system and of
the context, which the module wraps or returns as they are.

Columns:

- **identifier**: the exported error value or constant.
- **test**: a test of the module's root package. Run one row with
  `cd startup && go test -count=1 -run '^TestX$' .`; a row whose test is in a file built only
  outside Unix runs on the Windows leg of CI.
- **mutant site**: the file and the function the mutant changes.
- **mutant**: `every A becomes B`, in that function's body only. For an error value, `B` is
  another error value of the module, so the package still builds and the error returned is one the
  test does not accept. For a limit, `B` is `A+1` or `A-1`: the bound moves by one, and the test
  holds it at its boundary. Where one direction leaves the behaviour unchanged (a read that keeps
  more bytes than the bound still refuses them), the row records the other.

| identifier | test | mutant site | mutant |
|---|---|---|---|
| `ErrNoBinding` | `TestBindingsReadEachReferenceFromItsBinding` | `startup/reader.go:ReadSecret` | every `ErrNoBinding` becomes `ErrNotSet` |
| `ErrTwoBindings` | `TestBindingsReadEachReferenceFromItsBinding` | `startup/reader.go:ReadSecret` | every `ErrTwoBindings` becomes `ErrNoBinding` |
| `ErrNotSet` | `TestReadEnvRefusals` | `startup/reader.go:ReadEnv` | every `ErrNotSet` becomes `ErrEmpty` |
| `ErrEmpty` | `TestReadEnvRefusals` | `startup/reader.go:admitValue` | every `ErrEmpty` becomes `ErrLimit` |
| `ErrLimit` | `TestAdmitValueHoldsTheBound` | `startup/reader.go:admitValue` | every `ErrLimit` becomes `ErrEmpty` |
| `MaxSecretBytes` | `TestAdmitValueHoldsTheBound` | `startup/reader.go:admitValue` | every `MaxSecretBytes` becomes `MaxSecretBytes+1` |
| `MaxSecretBytes` | `TestReadFileRefusals` | `startup/reader.go:ReadFileWith` | every `MaxSecretBytes` becomes `MaxSecretBytes-1` |
| `ErrNotRegular` | `TestReadFileRefusals` | `startup/reader.go:checkFile` | every `ErrNotRegular` becomes `ErrWorldReadable` |
| `ErrWorldReadable` | `TestReadFileRefusesAWorldReadableFile` | `startup/permissions_unix.go:checkPermissions` | every `ErrWorldReadable` becomes `ErrWorldWritable` |
| `ErrWorldWritable` | `TestReadFileRefusesAWorldWritableFile` | `startup/permissions_unix.go:checkPermissions` | every `ErrWorldWritable` becomes `ErrWorldReadable` |
| `ErrPermissionsUnknown` | `TestReadFileRefusesEveryFileOutsideUnix` | `startup/permissions_other.go:checkPermissions` | every `ErrPermissionsUnknown` becomes `ErrWorldReadable` |
| `ErrNotDecodable` | `TestSecretRefusesToBeDecoded` | `startup/secret.go:UnmarshalJSON` | every `ErrNotDecodable` becomes `ErrDocument` |
| `ErrNotDecodable` | `TestSecretRefusesToBeDecoded` | `startup/secret.go:UnmarshalText` | every `ErrNotDecodable` becomes `ErrDocument` |
| `ErrSelector` | `TestReadResolvedRefusesASelectorItDoesNotRead` | `startup/resolved.go:ReadResolved` | every `ErrSelector` becomes `ErrDocument` |
| `ErrEnvironment` | `TestReadResolvedRefusesAnotherEnvironment` | `startup/resolved.go:ReadResolved` | every `ErrEnvironment` becomes `ErrSelector` |
| `ErrLimit` | `TestReadResolvedHoldsTheBounds` | `startup/resolved.go:ReadResolved` | every `ErrLimit` becomes `ErrDocument` |
| `MaxResolvedBytes` | `TestReadResolvedHoldsTheBounds` | `startup/resolved.go:ReadResolved` | every `MaxResolvedBytes` becomes `MaxResolvedBytes+1` |
| `ErrNotRegular` | `TestReadResolvedFileRefusesANamedPipe` | `startup/resolved.go:ReadResolvedFile` | every `ErrNotRegular` becomes `ErrDocument` |
| `MaxResolvedBytes` | `TestReadResolvedFile` | `startup/resolved.go:ReadResolvedFile` | every `MaxResolvedBytes` becomes `MaxResolvedBytes-1` |
| `ErrDocument` | `TestReadResolvedRefusesAMalformedMember` | `startup/resolved.go:closed` | every `ErrDocument` becomes `ErrSelector` |
| `ErrDocument` | `TestReadResolvedRefusesAMalformedMember` | `startup/resolved.go:malformed` | every `ErrDocument` becomes `ErrSelector` |
| `ErrDocument` | `TestReadResolvedRefusesWhatIsNotOneJSONValue` | `startup/resolved.go:scan` | every `ErrDocument` becomes `ErrLimit` |
| `ErrLimit` | `TestReadResolvedHoldsTheBounds` | `startup/resolved.go:scan` | every `ErrLimit` becomes `ErrDocument` |
| `MaxResolvedDepth` | `TestReadResolvedHoldsTheBounds` | `startup/resolved.go:scan` | every `MaxResolvedDepth` becomes `MaxResolvedDepth+1` |
