---
status: done
stage: 3
depends_on: []
created_at: 2026-11-21
---

# List mutators and disable them individually

## Motivation

The available mutators could not be checked from the CLI, and removing one mutator meant listing all the others with `-only`.

## Summary

- `mutant.Infos` (`internal/mutant/info.go`) defines each mutator's description and example in one place. `-list-mutators` prints the name, whether it is `default` or `opt-in`, the description, and the example, then exits without needing packages.
- The guide's list is not generated; a unit test (`TestInfos_MatchTheGuide`) checks that it names every mutator.
- `-disable` removes mutators from the default set. Combining it with `-only`, or an unknown name, exits with code 1.
- `TestListMutators` and `TestDisable_RemovesOperatorsFromTheDefaultSet` assert the list and that disabled mutators are not enumerated.
