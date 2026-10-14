# The served access-role strings are the vocabulary

The four Access role values — `owner`, `edit`, `view`, `public` — are the project's vocabulary. The prose in `DECK_SHARING_AND_ARCHITECTURE.md` that speaks of "collaborator", "viewer" and "guest" is describing the same four standings in different words and is not the vocabulary.

## Consequences

Clients read these strings to decide what a learner may do (`isOwner`, `canEdit`, and the library tabs), so renaming a value is a coordinated client change, not a refactor. When prose and wire disagree, the wire wins and the prose is corrected — do not "align" the code to the document, and see `GLOSSARY.md`, which lists the prose words under `_Avoid_`.
