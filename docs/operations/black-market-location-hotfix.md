# Black Market location hotfix

## Incident

Albion Data Client reports the in-game Black Market with location ID `3003`. The regular Caerleon marketplace is reported with location ID `3005`.

The receiver previously interpreted `3003` as a regular Caerleon city alias and canonicalized it to `3005`. As a result, Black Market buy orders were forwarded to the central API under the regular Caerleon market location.

## Correction

The catalog canonicalization now resolves explicit `black-market` entries before regular market aliases:

- `3003` remains `black_market`;
- `3005` remains the regular `caerleon` marketplace;
- aliases for other enabled regular markets continue to resolve to their marketplace IDs.

Regression coverage validates both the catalog mapping and normalized market orders.

## Production data repair

Before changing production data, the repair was validated against a Neon branch created from production.

The validated operation:

1. identifies buy-only raw observations stored under `3005` during the incident;
2. moves those observations to `3003`;
3. removes the corresponding false current snapshot rows from `3005`;
4. rebuilds the `3003` current snapshot from the latest raw observation per server, item and quality;
5. verifies that the reconstructed snapshot matches the raw audit trail.

No schema migration is required.

## Rollout verification

After updating the local receiver, capture one Black Market item and confirm:

- the receiver normalizes the location as `3003` / `black_market`;
- the central API stores a current row under `3003`;
- the Black Market opportunity scanner reports non-zero coverage;
- no new buy-only Black Market observations appear under `3005`.
