# Synthetic composition fixtures

Run `jpack experimental graph test --config internal/graph/testdata/composition/jpack.json --format json` from the Runtime checkout.

- `fan-in`: two nodes reference the same screening pack with independent inputs. The final pack explicitly requires both outcomes to be clear; either hit declines. Separate cases change each upstream outcome so an ignored input is detected. Missing upstream information remains unresolved.
- `fan-out`: one screening feeds two onboarding nodes. Both must run even though only one supplies the headline result.
- The adjacent `../project` fixture covers a sequence and unresolved evidence propagation.

Edges carry outcome IDs or evidence availability. They do not skip nodes, aggregate policy implicitly, or run branches concurrently. These fixtures are synthetic and authorize no real action.
