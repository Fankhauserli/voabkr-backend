# The served JSON name is the wire contract

A protocol buffer field is served to clients under its `json_name`, not under the buffer's own field name. The served name is the contract: clients send and read `isPublic`, and no response ever emits `is_public`.

Protojson also accepts the buffer's field name as an alias on input — `is_public` decodes to the same field — but an alias is tolerated input, not a second name the API serves. Because both spellings resolve to the same field number, a body carrying both is refused: the decoder reports a duplicate field and the request answers `400 Bad Request`. That refusal is deliberate. It is better than choosing one spelling silently, which is how a save body that declared both could appear to apply a visibility change while the server kept the other value.

## Considered options

**Decoding these bodies as plain JSON** was rejected. `encoding/json` matches the Go struct tags and ignores a key it does not recognise, so `is_public` would map to nothing and a request carrying only the alias would silently drop its visibility change. A toggle that appears to save and never takes effect is worse than a refusal.

**Loosening the decoder** — dropping the duplicate check, or letting one spelling win — was rejected for the same reason: it hides a client that disagrees with the contract about the field's name.

**Renaming the served fields to the buffer's names** was rejected: deployed clients read the served names, so a rename is a coordinated client change, not a refactor.

## Consequences

Clients declare only the served spellings in their request types; adding an alias back becomes a type error caught by their compiler rather than a `400` discovered in a browser. A request carries each field once. Unknown keys are still discarded, so a key with no counterpart on the server is accepted and ignored — documented for the Deck request rather than assumed. A Deck object the Deck reader serves emits every field, zero-valued where unset, so a field's presence is not a signal.
