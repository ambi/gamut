---
name: mutation-test-loop
description: Fill the surviving mutants gamut reports one at a time with tests, or accept them as equivalent. Use when asked, after a change, to fix mutation testing survivors or to fill weak spots in the tests.
---

# Filling survivors

gamut reports as survivors the small changes to the code (such as turning `<` into `<=`) that make no test fail.
A survivor is evidence of behavior the tests do not look at.
One at a time, either add a test that detects it, or accept it, with a reason, as one that no input can tell apart (equivalent).

## Steps

1. **Get the survivors.** For a pull request or work in progress, judge only the diff from the base branch.

   ```console
   gamut -reporter agent -since main ./path/to/pkg
   ```

   To look at everything, drop `-since`.
   Done when: you have the JSON's `survivors`. If it is empty, this skill is finished. If there are `warnings`, handle them in step 6.

2. **Pick one.** Take survivors with `critical: true` first (they fail CI through the quality gate). Handle survivors of the same `declaration` one after another.

3. **Write a test for its class.** Read `hint`, compare the `declaration` source with `before` and `after` in `diff`, and work out what the mutation changes before you write.

   | `class` | What is missing | The test to write |
   | --- | --- | --- |
   | `not covered` | No test runs this code | A test that calls the declaration and asserts its result |
   | `not infected` | Tests run it, but no input makes the mutation change a value | An input where the mutation changes a value (for a boundary comparison, one where both sides are equal) and an assertion on the result |
   | `not detected` | A value changes, but no assertion looks at it | An assertion on the very value the mutation changes (such as the whole return value) |
   | `no longer detected` | The test that detected it before (`previously_killed_by`) is gone | Bring the removed test back, or write one that checks the same thing |

   If there is a table-driven test, add a row to its table. Otherwise add a named case with `t.Run`.
   `tests` lists the tests that reached this mutant, with those containing the declaration's name first. They are the candidates to add to.
   Done when: you can explain why the test fails on the `after` code and passes on the `before` code.

4. **Check again.**

   ```console
   go test ./path/to/pkg
   gamut -mutant '<id>' ./path/to/pkg
   ```

   `-mutant` judges only that mutant, ignoring the cache.
   Done when: `go test` passes and the mutant is `KILLED`. If it is still `LIVED`, go back to step 3. If two rewrites still do not detect it, consider in step 5 whether it is equivalent.

5. **Accept it if it is equivalent.** Accept only when you can explain, from the range of inputs, that `before` and `after` give the same result for every input (for example, when `n == max`, returning `n` or `max` is the same).

   ```console
   gamut accept -reason '<why every input gives the same result>' '<id>'
   ```

   The acceptance is recorded in `gamut.accepted.json` at the module root, and the mutant is no longer counted.
   If you cannot explain it, do not accept it; report it to the user as missing tests.
   Done when: you accepted it, or wrote in the report why you did not.

6. **Handle the warnings.** Tests in `warnings` that `t.Skip` may skip, and tests that start the test binary as a child process, can mislead the verdicts. If a survivor involves only such a test, say so in the report.

7. **Move to the next survivor.** When you have handled them all, run the command of step 1 again.
   Done when: `survivors` is empty, or every remaining one is one you reported a reason not to accept.
