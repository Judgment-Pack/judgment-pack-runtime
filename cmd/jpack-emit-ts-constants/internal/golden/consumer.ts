// Reads the generated constants of three vectors as a consumer would.
// golden_test.go type-checks this file and the vectors' expected files with
// tsc --strict: every line marked @ts-expect-error must fail to type-check,
// and every other line must not.
import { Database, database } from "../../vectors/secret-omission/expected/generated/constants/prod";
import { pipeline } from "../../vectors/array/expected/generated/constants/staging";
import { JsonValue, Service, service } from "../../vectors/nested-object/expected/generated/constants/dev";

// Each entry has the kind's type, not its literal type.
const orders: Database = database.orders;
export const host: string = orders.host;
export const port: number = database.audit.port;
export const role: string | undefined = orders.grants?.analyst?.role;

// @ts-expect-error The password is at a secret position: the type has no such member.
export const password = orders.dbPassword;
// @ts-expect-error grantSecret is at a secret position below each grant.
export const grantSecret = orders.grants?.analyst?.grantSecret;
// @ts-expect-error Every member of tokens is at a secret position, so the map admits none.
export const withToken: Database = { host: "h", port: 1, tokens: { tokenA: "x" } };

// @ts-expect-error The constant is read-only.
database.orders = orders;
// @ts-expect-error So is each entry.
orders.host = "h";
// @ts-expect-error No entry has this id.
export const missing = database.payments;

export const stageNames: readonly string[] = pipeline.release.stages.map((stage) => stage.name);
export const matrix: readonly (readonly number[])[] = pipeline.release.matrix ?? [];
// @ts-expect-error stageToken is at a secret position below each stage.
export const stageToken = pipeline.release.stages[0].stageToken;

const checkout: Service = service.checkout;
export const tier: "gold" | "silver" | "bronze" = checkout.tier;
export const meta: JsonValue | undefined = checkout.meta;
export const separator: string | undefined = checkout.labels?.["line-separator"];
export const fallbackPort: number | undefined = service.search.fallback?.port;
// @ts-expect-error The tier is one of the enum's values.
export const wrongTier: Service = { name: "n", tier: "platinum", endpoint: { host: "h", port: 1 } };
