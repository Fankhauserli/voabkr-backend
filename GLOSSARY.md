# voabkr

A Korean vocabulary study application (TOPIK preparation). Learners study Decks of Cards with spaced repetition, own the Decks they create, and share them with other learners.

## Language

### Learner accounts

**Account**:
A learner's identity in the application: their profile, settings, verification status, and the erasure that removes them.
_Avoid_: user, member, account holder. Note "profile" and "settings" name parts of an Account, not the whole; the `/user/*` routes and their response types are wire vocabulary, not domain terms.

### Decks and sharing

**Deck**:
A named collection of Cards that a learner studies as a unit.
_Avoid_: list, set, decklist

**System deck**:
A Deck with no owner, maintained by the project rather than by a learner. Readable by anyone when it is public; never edited, shared, or deleted through the application.
_Avoid_: curated deck, official deck, global deck

**Public deck**:
A Deck any learner may study without holding a share. Every System deck that is readable is public; a learner's own Deck is public only if its owner made it so.
_Avoid_: open deck, free deck. Note "shared" is not a synonym: sharing is a DeckShare.

**DeckShare**:
A grant of access to a Deck, made by its owner to another learner, carrying exactly one Permission.
_Avoid_: invite, collaborator, membership

**Permission**:
What a DeckShare grants: `view`, meaning the holder may study the Deck; or `edit`, meaning the holder may also add and change its Cards.
_Avoid_: access level, right

**Access role**:
A learner's standing on a single Deck: `owner` (created it), `edit` (holds an edit Permission), `view` (holds a view Permission), or `public` (holds no share).
_Avoid_: collaborator, viewer, guest, member

**Deck owner**:
The learner who created a Deck, and the only one who may share it, change its visibility, or delete it.
_Avoid_: author, creator, maintainer

### Study and scheduling

**Card**:
A single Korean–English vocabulary item belonging to exactly one Deck, optionally carrying a context sentence and an example.
_Avoid_: flashcard, entry, item

**Review**:
One learner's record of studying one Card: its Review schedule, and the Grade given each time it has been answered.
_Avoid_: study record, attempt, history

**Grade**:
A learner's answer to a Card while studying: Again, Hard, Good, or Easy.
_Avoid_: ease (ambiguous: the request field named `ease` carries the Grade, while the multiplier is the Efactor)

**Review schedule**:
When a Review is due next, and the state that decides it: its Interval, Efactor, and relearning step. A Card answered Again goes back to a short relearning step before it resumes its Interval.
_Avoid_: due date, next review

**Review queue**:
The Cards a learner is due to study, optionally within one Deck or since a given moment.
_Avoid_: due list, study queue, backlog. Note the queue holds Cards, not Reviews: a Card is in it when the learner's Review of that Card is due.

**Interval**:
The number of days until a Review is due again.
_Avoid_: gap, delay, spacing

**Efactor**:
The multiplier by which a Review's Interval grows as the learner keeps answering well.
_Avoid_: ease, ease factor
