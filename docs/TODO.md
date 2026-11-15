# TODO

Manual tasks and deferred maintenance discovered during scoped changes.

## SEO / repository metadata

- [ ] Upload `docs/assets/symmetry-social-preview.png` as the GitHub social
      preview. GitHub has no REST/GraphQL API for this: open
      **Settings → Social preview → Edit → Upload an image**, then confirm the
      card renders on the repository page. The image is 1280×640 and mirrors the
      README banner (`docs/assets/symmetry-banner.png`).

## Dead-code candidates

- [ ] Revalidate and remove the unused `JsonDocument` alias in
      `control/lib/symmetry_control/goals/schemas.ex:1431`
      (`SymmetryControl.Goals.GoalExternalWait`). The 0006h focused Control run
      reported the unused alias; a scoped search of that module found only its
      alias declaration. Leave removal and verification to separate maintenance.

- [ ] Revalidate the unused `replay_task` binding at
      `control/test/symmetry_control/orchestration_goal_admission_test.exs:1059`
      and `task` bindings at lines `2079` and `2170`. The 0007a `mix test` run
      reported these compiler warnings (`.tmp/0007a-session/control-test.log`).
      Preserve the fixture calls and leave any binding cleanup to separate
      maintenance.
- `docs/goals/README.md` index row for 0006h targets `0006h-credentialed-provider-accounting.md` while the file lives at `docs/goals/archive/0006h-credentialed-provider-accounting.md` (pre-existing dead link; disclosed in the 0006-final series-review receipt F8). Revalidate and fix in a separate maintenance change.
