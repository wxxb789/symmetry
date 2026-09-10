defmodule SymmetryControl.GoalsReadModelTest do
  use ExUnit.Case, async: true

  alias SymmetryControl.Goals.Goal
  alias SymmetryControl.Goals.ReadModel

  defmodule CursorRepo do
    def all(%Ecto.Query{from: %{source: {_source, SymmetryControl.Goals.Goal}}} = query) do
      send(self(), {:attention_goal_query, query})
      Process.get(:goals_read_model_attention_rows, [])
    end

    def all(_query), do: []

    def preload(goal, associations) do
      Enum.reduce(associations, goal, fn association, goal ->
        if is_list(Map.get(goal, association)), do: goal, else: Map.put(goal, association, [])
      end)
    end
  end

  test "projects lifecycle separately from execution and accepted outcome receipts" do
    goal = %{
      id: "goal-1",
      project_id: "project-1",
      title: "Ship the migration",
      state: "active",
      current_revision: 2,
      lock_version: 3,
      updated_at: ~U[2026-09-09 00:00:00Z]
    }

    related = %{
      revisions: [
        %{
          revision: 2,
          objective: "Ship the migration",
          non_goals: ["No provider-side writes"],
          acceptance_contract: %{"checks" => ["tests"]},
          authority_policy: %{"acceptance" => "operator"},
          execution_policy: %{"max_parallel_tasks" => 1},
          context_manifest: %{}
        }
      ],
      work_items: [
        %{
          id: "item-1",
          number: 1,
          title: "Implement",
          status: "done",
          required: true,
          admitted_revision: 2
        },
        %{
          id: "item-2",
          number: 2,
          title: "Validate",
          status: "done",
          required: true,
          admitted_revision: 2
        }
      ],
      dependencies: [%{work_item_id: "item-2", depends_on_id: "item-1"}],
      outcomes: [
        %{
          id: "outcome-1",
          goal_id: "goal-1",
          goal_revision: 2,
          work_item_id: "item-1",
          producing_task_id: "task-1",
          producing_run_id: "run-1",
          subject_hash: :crypto.hash(:sha256, "subject-1"),
          evidence_ids: ["evidence-1"],
          disposition: "accepted",
          reason: "Validator accepted the exact subject."
        },
        %{
          id: "outcome-2",
          goal_id: "goal-1",
          goal_revision: 2,
          work_item_id: "item-2",
          producing_task_id: "task-2",
          producing_run_id: "run-2",
          subject_hash: :crypto.hash(:sha256, "subject-2"),
          evidence_ids: [],
          disposition: "rejected",
          reason: "Required check failed."
        }
      ],
      decisions: [
        %{
          id: "decision-1",
          goal_revision: 2,
          kind: "scope",
          state: "open",
          question: "Should the scope include the follow-up?",
          options: [%{"id" => "yes", "label" => "Yes", "consequence" => "Expand"}]
        }
      ],
      tasks: [
        %{
          id: "task-2",
          work_item_id: "item-2",
          goal_id: "goal-1",
          goal_revision: 1,
          context_snapshot_id: "snapshot-2",
          purpose: "validate",
          state: "running",
          current_generation: 1,
          attempt_generation: 1,
          result: %{"kind" => "blocked", "blocker" => "waiting_external"}
        }
      ],
      runs: [
        %{
          id: "run-2",
          task_id: "task-2",
          generation: 1,
          state: "running",
          runtime_id: "runtime-1"
        }
      ],
      runtimes: [%{id: "runtime-1", status: "offline"}]
    }

    projection = ReadModel.project(goal, related)

    assert projection.state == "active"

    assert projection.lifecycle == %{
             state: "active",
             terminal?: false,
             scheduling_paused?: false,
             immutable?: false
           }

    item_1 = Enum.find(projection.work_items, &(&1.id == "item-1"))
    item_2 = Enum.find(projection.work_items, &(&1.id == "item-2"))
    assert item_1.accepted? == true
    refute item_1.integration
    assert item_2.accepted? == false
    assert item_2.board_status == "done"
    assert item_2.execution.state == "running"
    assert item_2.execution.task.result.kind == "blocked"
    assert item_2.execution.task.result.blocker == "waiting_external"

    reasons = projection.blocker_reasons
    assert "waiting_decision" in reasons
    assert "validation_failed" in reasons
    assert "stale_context" in reasons
    refute "runtime_unavailable" in reasons
    refute "waiting_external" in reasons
    refute projection.completion.ready?
    refute "achieve" in projection.allowed_actions
  end

  test "projects a current planning Task and Run without fabricating WorkItem execution" do
    projection =
      ReadModel.project(
        %{id: "goal-plan", state: "draft", current_revision: 1},
        %{
          revisions: [%{revision: 1, execution_policy: %{}}],
          tasks: [
            %{
              id: "plan-task",
              goal_id: "goal-plan",
              goal_revision: 1,
              work_item_id: nil,
              validation_of_task_id: nil,
              purpose: "plan",
              state: "running",
              current_generation: 1,
              attempt_generation: 1
            }
          ],
          runs: [
            %{
              id: "plan-run",
              task_id: "plan-task",
              generation: 1,
              state: "running",
              runtime_id: "plan-runtime"
            }
          ],
          runtimes: [%{id: "plan-runtime", status: "online"}]
        }
      )

    assert projection.execution.task_count == 1
    assert projection.execution.active_task_count == 1
    assert projection.execution.run_count == 1
    assert projection.planning.task.id == "plan-task"
    assert projection.planning.task.work_item_id == nil
    assert projection.planning.run.id == "plan-run"
    assert projection.work_items == []
    refute "request_plan" in projection.allowed_actions
  end

  test "projects only current, unresolved automatic admission blockers" do
    event = %{
      id: "blocked-event",
      revision: 1,
      kind: "automatic_admission_blocked",
      inserted_at: ~U[2026-09-09 00:00:00Z],
      payload: %{
        "work_item_id" => "item-1",
        "purpose" => "implement",
        "candidate_identity" => "item-1:implement:baseline",
        "source_identity" => "baseline",
        "admission_key" => "admission-key",
        "reason" => "context_budget_exceeded"
      },
      response: %{"reason" => "context_budget_exceeded"}
    }

    related = %{
      revisions: [%{revision: 1, execution_policy: %{}}],
      work_items: [%{id: "item-1", admitted_revision: 1, required: true}],
      events: [event]
    }

    active =
      ReadModel.project(
        %{id: "goal-automatic-blocker", state: "active", current_revision: 1},
        related
      )

    assert "automatic_admission_blocked" in active.blocker_reasons

    accepted =
      ReadModel.project(
        %{id: "goal-automatic-blocker", state: "active", current_revision: 1},
        Map.put(related, :outcomes, [
          %{goal_revision: 1, work_item_id: "item-1", disposition: "accepted"}
        ])
      )

    refute "automatic_admission_blocked" in accepted.blocker_reasons

    replaced =
      ReadModel.project(
        %{id: "goal-automatic-blocker", state: "active", current_revision: 1},
        Map.put(related, :tasks, [
          %{
            id: "replacement-task",
            goal_id: "goal-automatic-blocker",
            goal_revision: 1,
            work_item_id: "item-1",
            purpose: "implement",
            admission_key: "manual-replacement-key",
            inserted_at: ~U[2026-09-09 00:00:01Z],
            state: "queued"
          }
        ])
      )

    refute "automatic_admission_blocked" in replaced.blocker_reasons

    terminal =
      ReadModel.project(
        %{id: "goal-automatic-blocker", state: "achieved", current_revision: 1},
        related
      )

    refute "automatic_admission_blocked" in terminal.blocker_reasons
  end

  test "projects the current durable automatic reconciliation deferral" do
    wake_at = ~U[2026-09-09 00:01:00Z]

    related = %{
      revisions: [%{revision: 1, execution_policy: %{}}],
      events: [
        %{
          id: "deferred-event",
          sequence: 2,
          revision: 1,
          kind: "automatic_reconciliation_deferred",
          payload: %{
            "deferred_identity" => "goal:goal_admission_disabled",
            "reason" => "goal_admission_disabled"
          },
          response: %{
            "reason" => "goal_admission_disabled",
            "next_wake_at" => DateTime.to_iso8601(wake_at)
          }
        }
      ]
    }

    deferred =
      ReadModel.project(
        %{
          id: "goal-automatic-deferred",
          state: "active",
          current_revision: 1,
          next_wake_at: wake_at
        },
        related
      )

    blocker = Enum.find(deferred.blockers, &(&1.reason == "automatic_reconciliation_deferred"))

    assert blocker.details == [
             %{
               event_id: "deferred-event",
               reason: "goal_admission_disabled",
               next_wake_at: wake_at
             }
           ]

    quiescent =
      ReadModel.project(
        %{id: "goal-automatic-deferred", state: "active", current_revision: 1, next_wake_at: nil},
        related
      )

    refute "automatic_reconciliation_deferred" in quiescent.blocker_reasons
  end

  test "a successful planning settlement is visible through its Decision without duplicate attention" do
    task = %{
      id: "plan-task",
      goal_id: "goal-plan-settlement",
      goal_revision: 1,
      work_item_id: nil,
      validation_of_task_id: nil,
      purpose: "plan",
      state: "completed",
      current_generation: 1,
      attempt_generation: 1
    }

    run = %{id: "plan-run", task_id: task.id, generation: 1, state: "completed"}

    event = %{
      id: "plan-settlement-event",
      sequence: 1,
      kind: "task_settled",
      revision: 1,
      payload: %{
        "task_id" => task.id,
        "run_id" => run.id,
        "generation" => 1,
        "state" => "completed",
        "goal_revision" => 1
      },
      response: %{
        "task_id" => task.id,
        "run_id" => run.id,
        "generation" => 1,
        "goal_revision" => 1,
        "settlement" => "plan_proposed",
        "decision_id" => "plan-decision",
        "result_kind" => "plan_proposed",
        "evidence_refs" => []
      }
    }

    projection =
      ReadModel.project(
        %{id: "goal-plan-settlement", state: "draft", current_revision: 1},
        %{
          revisions: [%{revision: 1, execution_policy: %{}}],
          decisions: [%{id: "plan-decision", goal_revision: 1, kind: "plan", state: "open"}],
          tasks: [task],
          runs: [run],
          task_settled_events: [event]
        }
      )

    assert projection.planning.task.id == task.id
    assert projection.planning.run.id == run.id
    assert [%{id: "plan-decision", open?: true}] = projection.decisions
    assert projection.settlement_attention == []
    refute "request_plan" in projection.allowed_actions

    rejected =
      ReadModel.project(
        %{id: "goal-plan-settlement", state: "draft", current_revision: 1},
        %{
          revisions: [%{revision: 1, execution_policy: %{}}],
          decisions: [
            %{
              id: "plan-decision",
              goal_revision: 1,
              kind: "plan",
              state: "resolved",
              resolution: %{"option_id" => "reject"}
            }
          ],
          tasks: [task],
          runs: [run],
          task_settled_events: [event]
        }
      )

    assert "request_plan" in rejected.allowed_actions
  end

  test "multiple rejected planning settlements do not leave request_plan pending" do
    task = fn id ->
      %{
        id: id,
        goal_id: "goal-plan-retry",
        goal_revision: 1,
        work_item_id: nil,
        validation_of_task_id: nil,
        purpose: "plan",
        state: "completed",
        current_generation: 1
      }
    end

    tasks = [task.("plan-task-one"), task.("plan-task-two")]

    events =
      Enum.map(tasks, fn planning_task ->
        run_id = planning_task.id <> "-run"

        %{
          id: planning_task.id <> "-settled",
          sequence: if(planning_task.id == "plan-task-one", do: 1, else: 2),
          kind: "task_settled",
          revision: 1,
          payload: %{
            "task_id" => planning_task.id,
            "run_id" => run_id,
            "generation" => 1,
            "state" => "completed",
            "goal_revision" => 1
          },
          response: %{
            "task_id" => planning_task.id,
            "run_id" => run_id,
            "generation" => 1,
            "goal_revision" => 1,
            "settlement" => "historical_result",
            "result_kind" => "plan_proposed",
            "evidence_refs" => []
          }
        }
      end)

    projection =
      ReadModel.project(
        %{id: "goal-plan-retry", state: "draft", current_revision: 1},
        %{
          revisions: [%{revision: 1, execution_policy: %{}}],
          tasks: tasks,
          runs:
            Enum.map(tasks, fn planning_task ->
              %{
                id: planning_task.id <> "-run",
                task_id: planning_task.id,
                generation: 1,
                state: "completed"
              }
            end),
          decisions: [
            %{
              id: "decision-one",
              goal_revision: 1,
              kind: "plan",
              state: "resolved",
              resolution: %{"option_id" => "reject"}
            },
            %{
              id: "decision-two",
              goal_revision: 1,
              kind: "plan",
              state: "resolved",
              resolution: %{"option_id" => "reject"}
            }
          ],
          task_settled_events: events
        }
      )

    assert "request_plan" in projection.allowed_actions
  end

  test "projects a valid typed TaskResult nested in a run result and reads its external blocker" do
    subject = subject()

    external_blocker = %{
      "kind" => "external",
      "resource_id" => subject["resource_id"],
      "external_ref" => "ci://build/42",
      "next_check_at" => "2026-09-09T00:00:00Z"
    }

    result = task_result("blocked", subject, blocker: external_blocker)

    projection =
      ReadModel.project(
        %{id: "goal-nested-result", state: "active", current_revision: 1},
        %{
          revisions: [%{revision: 1, execution_policy: %{}}],
          work_items: [%{id: "item-1", required: true, admitted_revision: 1}],
          tasks: [
            %{
              id: "task-1",
              work_item_id: "item-1",
              state: "completed",
              result: result
            }
          ],
          runs: [
            %{
              id: "run-1",
              task_id: "task-1",
              generation: 1,
              state: "completed",
              result: %{
                "summary" => "native turn completed",
                "task_result" => result
              }
            }
          ]
        }
      )

    item = hd(projection.work_items)
    assert item.execution.task.result == item.execution.run.result
    assert item.execution.run.result.kind == "blocked"
    assert item.execution.run.result.summary == "Waiting for an external check."
    assert item.execution.run.result.blocker == external_blocker

    refute "waiting_external" in projection.blocker_reasons
  end

  test "projects an unsupported external check from its immutable wait ledger" do
    subject = subject()

    projection =
      ReadModel.project(
        %{id: "goal-external-wait", state: "active", current_revision: 1},
        %{
          revisions: [%{revision: 1, execution_policy: %{}}],
          work_items: [%{id: "item-1", required: true, admitted_revision: 1}],
          external_waits: [
            %{
              id: "wait-1",
              goal_revision: 1,
              work_item_id: "item-1",
              task_id: "task-1",
              run_id: "run-1",
              run_generation: 1,
              result_id: "result-1",
              state: "unsupported",
              resource_id: subject["resource_id"],
              external_ref: "ci://build/42",
              subject: subject,
              source_ref: %{
                "kind" => "task_result",
                "task_id" => "task-1",
                "run_id" => "run-1",
                "generation" => 1,
                "result_id" => "result-1"
              },
              next_check_at: nil,
              check_seq: 0
            }
          ]
        }
      )

    assert "unsupported_external_check" in projection.blocker_reasons
    refute "waiting_external" in projection.blocker_reasons

    assert [%{id: "wait-1", state: "unsupported", reason: "unsupported_external_check"} = wait] =
             projection.external_waits

    assert wait.subject == subject
    assert wait.resource_id == subject["resource_id"]
    assert wait.external_ref == "ci://build/42"
    assert wait.next_check_at == nil
    assert wait.source_ref["task_id"] == "task-1"
  end

  test "does not derive a Goal blocker from an untrusted raw candidate result" do
    candidate =
      task_result("candidate_completion", subject(),
        blocker: %{
          "kind" => "external",
          "resource_id" => "00000000-0000-4000-8000-000000000001",
          "external_ref" => "ci://untrusted",
          "next_check_at" => "2026-09-09T00:00:00Z"
        }
      )

    projection =
      ReadModel.project(
        %{id: "goal-untrusted-result", state: "active", current_revision: 1},
        %{
          revisions: [%{revision: 1, execution_policy: %{}}],
          work_items: [
            %{id: "item-1", required: true, admitted_revision: 1, orchestration_task_id: "task-1"}
          ],
          tasks: [
            %{
              id: "task-1",
              goal_id: "goal-untrusted-result",
              work_item_id: "item-1",
              goal_revision: 1,
              current_generation: 1,
              state: "completed"
            }
          ],
          runs: [
            %{
              id: "run-1",
              task_id: "task-1",
              generation: 1,
              state: "completed",
              result: %{"task_result" => candidate}
            }
          ]
        }
      )

    item = hd(projection.work_items)
    assert item.execution.run.result.blocker["kind"] == "external"
    assert projection.settlement_attention == []
    refute "waiting_external" in projection.blocker_reasons
  end

  test "does not fall back to outer result fields for a malformed typed TaskResult" do
    projection =
      ReadModel.project(
        %{id: "goal-malformed-result", state: "active", current_revision: 1},
        %{
          revisions: [%{revision: 1, execution_policy: %{}}],
          work_items: [%{id: "item-1", required: true, admitted_revision: 1}],
          tasks: [%{id: "task-1", work_item_id: "item-1", state: "completed"}],
          runs: [
            %{
              id: "run-1",
              task_id: "task-1",
              generation: 1,
              state: "completed",
              result: %{
                "kind" => "blocked",
                "blocker" => "waiting_external",
                "task_result" => %{
                  "kind" => "candidate_completion"
                }
              }
            }
          ]
        }
      )

    item = hd(projection.work_items)
    assert item.execution.run.result == nil
    refute "waiting_external" in projection.blocker_reasons
  end

  test "surfaces no_verified_progress and optional failed settlements without adding completion blockers" do
    no_progress =
      settled_projection("no_verified_progress",
        required?: false,
        reason: "No new subject or verified evidence was produced.",
        proposed_next_action: %{
          "kind" => "repair",
          "work_item_id" => "item-1",
          "reason" => "Try a bounded repair."
        },
        proposed_next_action_status: "proposal_only"
      )

    assert no_progress.attention_reasons == ["no_verified_progress"]

    assert [attention] = no_progress.settlement_attention
    assert attention.settlement == "no_verified_progress"
    assert attention.reason == "No new subject or verified evidence was produced."
    assert attention.proposed_next_action_status == "proposal_only"
    assert attention.proposed_next_action["kind"] == "repair"
    refute "no_verified_progress" in no_progress.blocker_reasons

    failed = settled_projection("failed", required?: false, reason: "The native turn failed.")

    assert failed.attention_reasons == ["failed"]
    assert [%{required?: false, settlement: "failed"}] = failed.settlement_attention
    refute "failed" in failed.blocker_reasons
  end

  test "visible optional validation failure does not hide an achievable Goal action" do
    subject = subject()
    subject_hash = SymmetryControl.RequestHash.canonical(subject)

    projection =
      final_acceptance_related(subject, subject_hash,
        work_items: [
          %{id: "integration-item", required: false, integration: true, admitted_revision: 1},
          %{id: "optional-item", required: false, integration: false, admitted_revision: 1}
        ],
        outcomes: [
          %{
            id: "integration-outcome",
            goal_revision: 1,
            work_item_id: "integration-item",
            disposition: "accepted",
            subject_hash: subject_hash
          },
          %{
            id: "optional-rejected",
            goal_revision: 1,
            work_item_id: "optional-item",
            disposition: "rejected",
            reason: "Optional validation failed."
          }
        ]
      )
      |> then(
        &ReadModel.project(%{id: "goal-final-ready", state: "active", current_revision: 1}, &1)
      )

    assert "validation_failed" in projection.blocker_reasons
    assert projection.completion.ready?
    assert "achieve" in projection.allowed_actions
  end

  test "surfaces only typed blocked settlements that still need a disposition" do
    environment =
      settled_projection("blocked",
        blocker: %{
          "kind" => "environment",
          "code" => "tool_missing",
          "detail" => "Install the validator."
        },
        reason: "The validator is unavailable.",
        proposed_next_action: %{
          "kind" => "repair",
          "work_item_id" => "item-1",
          "reason" => "Restore the validator."
        },
        proposed_next_action_status: "proposal_only"
      )

    assert environment.attention_reasons == ["blocked"]

    assert [environment_attention] = environment.settlement_attention
    assert environment_attention.blocker["kind"] == "environment"
    assert environment_attention.reason == "The validator is unavailable."
    assert environment_attention.proposed_next_action_status == "proposal_only"

    external =
      settled_projection("blocked",
        blocker: %{
          "kind" => "external",
          "resource_id" => "resource-1",
          "external_ref" => "ci://build/42",
          "next_check_at" => "2026-09-09T00:00:00Z"
        }
      )

    assert external.settlement_attention == []
    assert "waiting_external" in external.blocker_reasons

    decision =
      settled_projection("blocked",
        blocker: %{"kind" => "decision", "decision_id" => "decision-1"},
        decisions: [%{id: "decision-1", goal_revision: 1, state: "open"}]
      )

    assert decision.attention_reasons == ["blocked"]

    assert [%{blocker: %{"kind" => "decision", "decision_id" => "decision-1"}}] =
             decision.settlement_attention

    resolved_decision =
      settled_projection("blocked",
        blocker: %{"kind" => "decision", "decision_id" => "decision-1"},
        decisions: [%{id: "decision-1", goal_revision: 1, state: "resolved"}]
      )

    assert resolved_decision.settlement_attention == []
    assert resolved_decision.attention_reasons == []
  end

  test "surfaces repair and replan settlement receipts" do
    for settlement <- ["repair_required", "replan_required"] do
      projection =
        settled_projection(settlement,
          proposed_next_action: %{
            "kind" => "replan",
            "reason" => "The admitted plan needs revision."
          },
          proposed_next_action_status: "proposal_only"
        )

      assert projection.attention_reasons == [settlement]

      assert [%{settlement: ^settlement, proposed_next_action_status: "proposal_only"}] =
               projection.settlement_attention

      refute settlement in projection.blocker_reasons
    end
  end

  test "surfaces validation-pending and invalid terminal settlement receipts" do
    for settlement <- ["awaiting_validation", "invalid_task_result", "missing_result"] do
      projection = settled_projection(settlement)

      assert projection.attention_reasons == [settlement]
      assert [%{settlement: ^settlement}] = projection.settlement_attention
      refute settlement in projection.blocker_reasons
    end
  end

  test "clears settlement attention after replacement admission, acceptance, or amendment" do
    replacement =
      settled_projection("repair_required",
        work_items: [
          %{id: "item-1", required: true, admitted_revision: 1, orchestration_task_id: "task-2"}
        ],
        tasks: [
          settled_task(),
          %{
            id: "task-2",
            goal_id: "goal-settlement",
            work_item_id: "item-1",
            goal_revision: 1,
            current_generation: 0,
            state: "queued"
          }
        ]
      )

    assert replacement.settlement_attention == []

    accepted =
      settled_projection("awaiting_validation",
        outcomes: [
          %{id: "outcome-1", goal_revision: 1, work_item_id: "item-1", disposition: "accepted"}
        ]
      )

    assert accepted.settlement_attention == []

    amended = settled_projection("replan_required", current_revision: 2, receipt_revision: 1)
    assert amended.settlement_attention == []
    assert amended.attention_reasons == []
  end

  test "filters stale generations and terminal Goals and deduplicates replayed settlement receipts" do
    stale_generation =
      settled_projection("repair_required",
        task: %{settled_task() | current_generation: 2},
        run: settled_run("task-1", 1)
      )

    assert stale_generation.settlement_attention == []

    terminal = settled_projection("repair_required", state: "achieved")
    assert terminal.settlement_attention == []

    replayed = settled_projection("repair_required", replayed?: true)
    assert replayed.attention_reasons == ["repair_required"]
    assert [%{event_id: "settlement-event-1"}] = replayed.settlement_attention
  end

  test "a prior revision receipt does not mark current work accepted" do
    projection =
      ReadModel.project(
        %{id: "goal-2", state: "paused", current_revision: 2},
        %{
          revisions: [%{revision: 2, objective: "Current", execution_policy: %{}}],
          work_items: [%{id: "item-1", status: "done", required: true, admitted_revision: 2}],
          outcomes: [
            %{
              id: "old-outcome",
              goal_revision: 1,
              work_item_id: "item-1",
              disposition: "accepted",
              subject_hash: :crypto.hash(:sha256, "old"),
              evidence_ids: []
            }
          ]
        }
      )

    item = hd(projection.work_items)
    assert item.accepted? == false
    assert projection.accepted_outcomes == []
    assert projection.completion.accepted_required_items == 0
  end

  test "an accepted independent validation clears a prior rejected outcome blocker" do
    projection =
      ReadModel.project(
        %{id: "goal-validation-retry", state: "active", current_revision: 1},
        %{
          revisions: [%{revision: 1, execution_policy: %{}}],
          work_items: [%{id: "item-1", required: true, admitted_revision: 1}],
          outcomes: [
            %{
              id: "rejected-outcome",
              goal_revision: 1,
              work_item_id: "item-1",
              producing_task_id: "producer-1",
              validation_task_id: "validation-1",
              disposition: "rejected",
              reason: "The first independent validation failed."
            },
            %{
              id: "accepted-outcome",
              goal_revision: 1,
              work_item_id: "item-1",
              producing_task_id: "producer-2",
              validation_task_id: "validation-2",
              disposition: "accepted",
              reason: "A later independent validation accepted the new candidate."
            }
          ]
        }
      )

    assert hd(projection.work_items).accepted?
    assert projection.completion.accepted_required_items == 1
    assert projection.completion.rejected_outcomes == 1
    refute "validation_failed" in projection.blocker_reasons
  end

  test "graph marks dependency edges from accepted receipts, never board status" do
    projection =
      ReadModel.project(
        %{id: "goal-3", state: "active", current_revision: 1},
        %{
          revisions: [%{revision: 1, objective: "Graph", execution_policy: %{}}],
          work_items: [
            %{id: "item-a", status: "done", required: true, admitted_revision: 1},
            %{id: "item-b", status: "backlog", required: true, admitted_revision: 1}
          ],
          dependencies: [%{id: "dependency-1", work_item_id: "item-b", depends_on_id: "item-a"}],
          outcomes: []
        }
      )

    [edge] = projection.graph.edges
    assert edge.satisfied? == false
    assert edge.blocker == "waiting_dependency"
    assert "waiting_dependency" in projection.blocker_reasons
  end

  test "amended goals isolate current work while retaining old revision history" do
    projection =
      ReadModel.project(
        %{id: "goal-amended", state: "active", current_revision: 2},
        %{
          revisions: [
            %{revision: 1, objective: "Old objective", execution_policy: %{}},
            %{revision: 2, objective: "Amended objective", execution_policy: %{}}
          ],
          work_items: [
            %{
              id: "old-item",
              title: "Old item",
              status: "done",
              required: true,
              admitted_revision: 1
            },
            %{
              id: "current-item",
              title: "Current item",
              status: "backlog",
              required: true,
              admitted_revision: 2
            }
          ],
          dependencies: [
            %{id: "old-edge", work_item_id: "old-item", depends_on_id: "old-prerequisite"},
            %{id: "cross-revision-edge", work_item_id: "current-item", depends_on_id: "old-item"}
          ],
          outcomes: [
            %{
              id: "old-outcome",
              goal_revision: 1,
              work_item_id: "old-item",
              disposition: "accepted",
              subject_hash: :crypto.hash(:sha256, "old-subject"),
              evidence_ids: []
            }
          ],
          tasks: [
            %{
              id: "old-task",
              work_item_id: "old-item",
              goal_id: "goal-amended",
              goal_revision: 1,
              context_snapshot_id: "old-snapshot",
              purpose: "implement",
              state: "running",
              current_generation: 1,
              attempt_generation: 1
            }
          ],
          runs: [
            %{
              id: "old-run",
              task_id: "old-task",
              generation: 1,
              state: "running",
              runtime_id: "old-runtime"
            }
          ],
          runtimes: [%{id: "old-runtime", status: "offline"}]
        }
      )

    assert Enum.map(projection.work_items, & &1.id) == ["current-item"]
    assert Enum.map(projection.graph.nodes, & &1.id) == ["current-item"]
    assert projection.graph.edges == []
    assert projection.completion.required_items == 1
    assert projection.completion.accepted_required_items == 0
    refute "waiting_dependency" in projection.blocker_reasons
    refute "runtime_unavailable" in projection.blocker_reasons
    refute "stale_context" in projection.blocker_reasons

    assert Enum.map(projection.history.work_items, & &1.id) == ["old-item"]

    assert Enum.map(projection.history.dependencies, & &1.id) == [
             "old-edge",
             "cross-revision-edge"
           ]

    assert Enum.map(projection.history.outcomes, & &1.id) == ["old-outcome"]
  end

  test "completion is blocked by a nonterminal Task retained from a prior revision" do
    subject_hash = :crypto.hash(:sha256, "integration-subject")

    projection =
      ReadModel.project(
        %{id: "goal-history", state: "active", current_revision: 2},
        %{
          revisions: [%{revision: 2, execution_policy: %{}}],
          work_items: [%{id: "current", required: true, admitted_revision: 2}],
          outcomes: [
            %{
              id: "current-outcome",
              goal_revision: 2,
              work_item_id: "current",
              disposition: "accepted",
              subject_hash: subject_hash
            }
          ],
          tasks: [%{id: "old-task", goal_revision: 1, state: "running"}]
        }
      )

    refute projection.completion.no_nonterminal_tasks?
    refute projection.completion.ready?
    refute "achieve" in projection.allowed_actions
  end

  test "completion is not advertised without a subject-bound decision and eligible Goal evidence" do
    subject_hash = :crypto.hash(:sha256, "integration-subject")

    projection =
      ReadModel.project(
        %{id: "goal-final", state: "active", current_revision: 1},
        %{
          revisions: [
            %{
              revision: 1,
              execution_policy: %{"final_acceptance" => "operator"},
              acceptance_contract: %{
                "predicates" => [
                  %{"id" => "check", "kind" => "check", "validator_profile" => "test"}
                ]
              }
            }
          ],
          work_items: [%{id: "integration", required: true, admitted_revision: 1}],
          outcomes: [
            %{
              id: "integration-outcome",
              goal_revision: 1,
              work_item_id: "integration",
              disposition: "accepted",
              subject_hash: subject_hash
            }
          ]
        }
      )

    refute projection.completion.ready?
    refute "achieve" in projection.allowed_actions
  end

  test "only the current designated integration WorkItem can establish final acceptance" do
    subject_hash = :crypto.hash(:sha256, "integration-subject")

    completion_decision = %{
      id: "completion-decision",
      goal_revision: 1,
      kind: "completion",
      state: "resolved",
      subject_hash: subject_hash,
      resolution: %{"option_id" => "accept"},
      action_hash:
        SymmetryControl.RequestHash.canonical(%{
          kind: "completion",
          goal_id: "goal-integration",
          revision: 1,
          subject_hash: Base.encode16(subject_hash, case: :lower)
        })
    }

    related = %{
      revisions: [
        %{
          revision: 1,
          execution_policy: %{"final_acceptance" => "operator"},
          acceptance_contract: %{"predicates" => [%{"kind" => "operator_acceptance"}]}
        }
      ],
      work_items: [
        %{id: "normal-item", required: true, integration: false, admitted_revision: 1},
        %{id: "integration-item", required: false, integration: true, admitted_revision: 1},
        %{id: "optional-follow-up", required: false, integration: false, admitted_revision: 1},
        %{id: "optional-prerequisite", required: false, integration: false, admitted_revision: 1}
      ],
      dependencies: [
        %{
          id: "optional-edge",
          work_item_id: "optional-follow-up",
          depends_on_id: "optional-prerequisite"
        }
      ],
      decisions: [completion_decision],
      outcomes: [
        %{
          id: "normal-outcome",
          goal_revision: 1,
          work_item_id: "normal-item",
          disposition: "accepted",
          subject_hash: subject_hash
        }
      ]
    }

    normal_only =
      ReadModel.project(
        %{id: "goal-integration", state: "active", current_revision: 1},
        related
      )

    integration_item = Enum.find(normal_only.work_items, & &1.integration)
    assert integration_item.id == "integration-item"
    assert integration_item.integration
    refute Enum.find(normal_only.work_items, &(&1.id == "normal-item")).integration
    refute normal_only.completion.ready?
    refute "achieve" in normal_only.allowed_actions

    integration_accepted =
      ReadModel.project(
        %{id: "goal-integration", state: "active", current_revision: 1},
        put_in(related, [:outcomes], [
          %{
            id: "normal-outcome",
            goal_revision: 1,
            work_item_id: "normal-item",
            disposition: "accepted",
            subject_hash: subject_hash
          },
          %{
            id: "integration-outcome",
            goal_revision: 1,
            work_item_id: "integration-item",
            disposition: "accepted",
            subject_hash: subject_hash
          }
        ])
      )

    assert integration_accepted.completion.ready?
    assert "achieve" in integration_accepted.allowed_actions

    assert Enum.any?(
             integration_accepted.graph.edges,
             &(&1.id == "optional-edge" and not &1.satisfied?)
           )

    refute "waiting_dependency" in integration_accepted.blocker_reasons

    optional_task_blocks =
      ReadModel.project(
        %{id: "goal-integration", state: "active", current_revision: 1},
        put_in(related, [:outcomes], [
          %{
            id: "normal-outcome",
            goal_revision: 1,
            work_item_id: "normal-item",
            disposition: "accepted",
            subject_hash: subject_hash
          },
          %{
            id: "integration-outcome",
            goal_revision: 1,
            work_item_id: "integration-item",
            disposition: "accepted",
            subject_hash: subject_hash
          }
        ])
        |> Map.put(:tasks, [
          %{
            id: "optional-task",
            goal_id: "goal-integration",
            goal_revision: 1,
            work_item_id: "optional-follow-up",
            state: "running"
          }
        ])
      )

    refute optional_task_blocks.completion.no_nonterminal_tasks?
    refute optional_task_blocks.completion.ready?
    refute "achieve" in optional_task_blocks.allowed_actions
  end

  test "completion follows required dependency closure through optional WorkItems" do
    subject_hash = :crypto.hash(:sha256, "integration-subject")

    completion_decision = %{
      id: "completion-decision",
      goal_revision: 1,
      kind: "completion",
      state: "resolved",
      subject_hash: subject_hash,
      resolution: %{"option_id" => "accept"},
      action_hash:
        SymmetryControl.RequestHash.canonical(%{
          kind: "completion",
          goal_id: "goal-dependency-closure",
          revision: 1,
          subject_hash: Base.encode16(subject_hash, case: :lower)
        })
    }

    projection =
      ReadModel.project(
        %{id: "goal-dependency-closure", state: "active", current_revision: 1},
        %{
          revisions: [
            %{
              revision: 1,
              execution_policy: %{"final_acceptance" => "operator"},
              acceptance_contract: %{"predicates" => [%{"kind" => "operator_acceptance"}]}
            }
          ],
          work_items: [
            %{id: "required", required: true, integration: false, admitted_revision: 1},
            %{id: "integration", required: false, integration: true, admitted_revision: 1},
            %{id: "optional-one", required: false, integration: false, admitted_revision: 1},
            %{id: "optional-two", required: false, integration: false, admitted_revision: 1}
          ],
          dependencies: [
            %{id: "required-edge", work_item_id: "required", depends_on_id: "optional-one"},
            %{id: "transitive-edge", work_item_id: "optional-one", depends_on_id: "optional-two"}
          ],
          decisions: [completion_decision],
          outcomes: [
            %{
              id: "required-outcome",
              goal_revision: 1,
              work_item_id: "required",
              disposition: "accepted",
              subject_hash: subject_hash
            },
            %{
              id: "integration-outcome",
              goal_revision: 1,
              work_item_id: "integration",
              disposition: "accepted",
              subject_hash: subject_hash
            },
            %{
              id: "optional-one-outcome",
              goal_revision: 1,
              work_item_id: "optional-one",
              disposition: "accepted",
              subject_hash: subject_hash
            }
          ]
        }
      )

    refute projection.completion.ready?
    refute "achieve" in projection.allowed_actions
    assert "waiting_dependency" in projection.blocker_reasons
  end

  test "completion exposes an unsatisfied selected integration dependency" do
    subject_hash = :crypto.hash(:sha256, "integration-subject")

    decision = %{
      id: "completion-decision",
      goal_revision: 1,
      kind: "completion",
      state: "resolved",
      subject_hash: subject_hash,
      resolution: %{"option_id" => "accept"},
      action_hash:
        SymmetryControl.RequestHash.canonical(%{
          kind: "completion",
          goal_id: "goal-integration-dependency",
          revision: 1,
          subject_hash: Base.encode16(subject_hash, case: :lower)
        })
    }

    projection =
      ReadModel.project(
        %{id: "goal-integration-dependency", state: "active", current_revision: 1},
        %{
          revisions: [
            %{
              revision: 1,
              execution_policy: %{"final_acceptance" => "operator"},
              acceptance_contract: %{"predicates" => [%{"kind" => "operator_acceptance"}]}
            }
          ],
          work_items: [
            %{id: "required", required: true, integration: false, admitted_revision: 1},
            %{id: "integration", required: false, integration: true, admitted_revision: 1},
            %{
              id: "optional-prerequisite",
              required: false,
              integration: false,
              admitted_revision: 1
            },
            %{
              id: "required-prerequisite",
              required: false,
              integration: false,
              admitted_revision: 1
            }
          ],
          dependencies: [
            %{
              id: "required-edge",
              work_item_id: "required",
              depends_on_id: "required-prerequisite"
            },
            %{
              id: "integration-edge",
              work_item_id: "integration",
              depends_on_id: "optional-prerequisite"
            }
          ],
          decisions: [decision],
          outcomes: [
            %{
              id: "required-outcome",
              goal_revision: 1,
              work_item_id: "required",
              disposition: "accepted",
              subject_hash: subject_hash
            },
            %{
              id: "integration-outcome",
              goal_revision: 1,
              work_item_id: "integration",
              disposition: "accepted",
              subject_hash: subject_hash
            }
          ]
        }
      )

    refute projection.completion.ready?
    refute "achieve" in projection.allowed_actions
    assert "waiting_dependency" in projection.blocker_reasons

    assert %{reason: "waiting_dependency", count: 2, details: details} =
             Enum.find(projection.blockers, &(&1.reason == "waiting_dependency"))

    assert %{work_item_id: "integration", depends_on_id: "optional-prerequisite", revision: 1} in details

    assert %{work_item_id: "required", depends_on_id: "required-prerequisite", revision: 1} in details
  end

  test "runtime availability only considers the current active Task generation" do
    queued_retry =
      ReadModel.project(
        %{id: "goal-runtime", state: "active", current_revision: 1},
        %{
          revisions: [%{revision: 1, execution_policy: %{}}],
          work_items: [%{id: "item-1", required: false, admitted_revision: 1}],
          tasks: [
            %{
              id: "task-1",
              work_item_id: "item-1",
              goal_id: "goal-runtime",
              goal_revision: 1,
              context_snapshot_id: "snapshot-1",
              current_generation: 2,
              state: "queued"
            }
          ],
          runs: [
            %{
              id: "run-old",
              task_id: "task-1",
              generation: 1,
              state: "completed",
              runtime_id: "runtime-1"
            }
          ],
          runtimes: [%{id: "runtime-1", status: "offline"}]
        }
      )

    refute "runtime_unavailable" in queued_retry.blocker_reasons

    assigned_current =
      ReadModel.project(
        %{id: "goal-runtime", state: "active", current_revision: 1},
        %{
          revisions: [%{revision: 1, execution_policy: %{}}],
          work_items: [%{id: "item-1", required: false, admitted_revision: 1}],
          tasks: [
            %{
              id: "task-1",
              work_item_id: "item-1",
              goal_id: "goal-runtime",
              goal_revision: 1,
              context_snapshot_id: "snapshot-1",
              current_generation: 2,
              state: "assigned"
            }
          ],
          runs: [
            %{
              id: "run-current",
              task_id: "task-1",
              generation: 2,
              state: "assigned",
              runtime_id: "runtime-1"
            },
            %{
              id: "run-future",
              task_id: "task-1",
              generation: 3,
              state: "completed",
              runtime_id: "runtime-1"
            }
          ],
          runtimes: [%{id: "runtime-1", status: "offline"}]
        }
      )

    assert "runtime_unavailable" in assigned_current.blocker_reasons

    missing_runtime =
      ReadModel.project(
        %{id: "goal-runtime", state: "active", current_revision: 1},
        %{
          revisions: [%{revision: 1, execution_policy: %{}}],
          work_items: [%{id: "item-1", required: false, admitted_revision: 1}],
          tasks: [
            %{
              id: "task-1",
              work_item_id: "item-1",
              goal_id: "goal-runtime",
              goal_revision: 1,
              context_snapshot_id: "snapshot-1",
              current_generation: 2,
              state: "assigned"
            }
          ],
          runs: [
            %{
              id: "run-current",
              task_id: "task-1",
              generation: 2,
              state: "assigned",
              runtime_id: "missing-runtime"
            }
          ]
        }
      )

    refute "runtime_unavailable" in missing_runtime.blocker_reasons
  end

  test "accepted current WorkItems do not retain external availability blockers" do
    projection =
      ReadModel.project(
        %{id: "goal-external", state: "active", current_revision: 1},
        %{
          revisions: [%{revision: 1, execution_policy: %{}}],
          work_items: [
            %{id: "item-1", required: false, external_available: false, admitted_revision: 1}
          ],
          outcomes: [
            %{
              id: "accepted-outcome",
              goal_revision: 1,
              work_item_id: "item-1",
              disposition: "accepted"
            }
          ]
        }
      )

    refute "waiting_external" in projection.blocker_reasons
  end

  test "final evidence requires a validator Task from the current Goal revision" do
    subject = subject()
    subject_hash = SymmetryControl.RequestHash.canonical(subject)

    related =
      final_acceptance_related(subject, subject_hash,
        acceptance_contract: %{
          "predicates" => [
            %{"id" => "goal-check", "kind" => "check", "validator_profile" => "test"},
            %{"id" => "operator", "kind" => "operator_acceptance"}
          ]
        },
        tasks: [
          %{
            id: "validation-task",
            work_item_id: "integration-item",
            goal_id: "goal-final-ready",
            goal_revision: 0,
            purpose: "validate",
            current_generation: 1,
            state: "completed"
          }
        ],
        runs: [
          %{id: "validation-run", task_id: "validation-task", generation: 1, state: "completed"}
        ],
        evidence: [goal_check_evidence(subject, subject_hash)]
      )

    old_validator =
      ReadModel.project(%{id: "goal-final-ready", state: "active", current_revision: 1}, related)

    refute old_validator.completion.ready?
    refute "achieve" in old_validator.allowed_actions

    current_validator =
      ReadModel.project(
        %{id: "goal-final-ready", state: "active", current_revision: 1},
        put_in(related, [:tasks, Access.at(0), :goal_revision], 1)
      )

    assert current_validator.completion.ready?
    assert "achieve" in current_validator.allowed_actions
  end

  test "zero required WorkItems can complete through the designated integration outcome" do
    subject = subject()
    subject_hash = SymmetryControl.RequestHash.canonical(subject)

    projection =
      final_acceptance_related(subject, subject_hash)
      |> then(
        &ReadModel.project(%{id: "goal-final-ready", state: "active", current_revision: 1}, &1)
      )

    assert projection.completion.required_items == 0
    assert projection.completion.all_required_accepted?
    assert projection.completion.ready?
    assert "achieve" in projection.allowed_actions
  end

  test "a technically ready integration candidate without final operator authority remains visible" do
    subject = subject()
    subject_hash = SymmetryControl.RequestHash.canonical(subject)

    projection =
      final_acceptance_related(subject, subject_hash, decisions: [])
      |> then(
        &ReadModel.project(%{id: "goal-final-ready", state: "active", current_revision: 1}, &1)
      )

    assert [%{reason: "final_acceptance_required", ids: ["integration-item"]}] =
             projection.blockers

    refute projection.completion.ready?
    refute "achieve" in projection.allowed_actions
  end

  test "deterministic completion with unmet required evidence is an inspectable attention blocker" do
    subject = subject()
    subject_hash = SymmetryControl.RequestHash.canonical(subject)

    related =
      final_acceptance_related(subject, subject_hash,
        authority_policy: %{"operator_required_for_completion" => false},
        acceptance_contract: %{
          "predicates" => [
            %{"id" => "goal-check", "kind" => "check", "validator_profile" => "test"}
          ]
        },
        decisions: []
      )
      |> put_in([:revisions, Access.at(0), :execution_policy], %{
        "final_acceptance" => "deterministic"
      })

    projection =
      ReadModel.project(%{id: "goal-final-ready", state: "active", current_revision: 1}, related)

    assert [%{reason: "required_predicates_unmet", ids: ["integration-item"]} = blocker] =
             projection.blockers

    assert blocker.details == [
             %{
               work_item_id: "integration-item",
               subject_hash: "sha256:" <> Base.encode16(subject_hash, case: :lower),
               predicate_ids: ["goal-check"],
               predicate_kinds: ["check"]
             }
           ]

    refute "final_acceptance_required" in projection.blocker_reasons
    refute projection.completion.ready?
    refute "achieve" in projection.allowed_actions

    goal =
      struct(Goal, %{
        id: "goal-final-ready",
        project_id: "project-final-ready",
        title: "Ready only with evidence",
        state: "active",
        current_revision: 1,
        lock_version: 1,
        updated_at: ~U[2026-09-10 00:00:00Z],
        revisions: related.revisions,
        work_items: related.work_items,
        dependencies: Map.get(related, :dependencies, []),
        outcomes: related.outcomes,
        decisions: related.decisions
      })

    Process.put(:goals_read_model_attention_rows, [goal])
    on_exit(fn -> Process.delete(:goals_read_model_attention_rows) end)

    assert {:ok, %{entries: [entry]}} = ReadModel.attention(repo: CursorRepo, limit: 1)
    assert entry.goal_id == goal.id
    assert entry.blocker_reasons == ["required_predicates_unmet"]
    refute entry.ready_to_achieve?

    satisfied =
      related
      |> Map.put(:tasks, [
        %{
          id: "validation-task",
          work_item_id: "integration-item",
          goal_id: "goal-final-ready",
          goal_revision: 1,
          purpose: "validate",
          current_generation: 1,
          state: "completed"
        }
      ])
      |> Map.put(:runs, [
        %{id: "validation-run", task_id: "validation-task", generation: 1, state: "completed"}
      ])
      |> Map.put(:evidence, [goal_check_evidence(subject, subject_hash)])

    satisfied_projection =
      ReadModel.project(
        %{id: "goal-final-ready", state: "active", current_revision: 1},
        satisfied
      )

    assert satisfied_projection.completion.ready?
    assert "achieve" in satisfied_projection.allowed_actions
    refute "required_predicates_unmet" in satisfied_projection.blocker_reasons
  end

  test "an accepted integration outcome is required and remains visible in attention" do
    subject_hash = :crypto.hash(:sha256, "missing-integration-outcome")

    related =
      final_acceptance_related(%{}, subject_hash,
        outcomes: [],
        decisions: []
      )

    projection =
      ReadModel.project(
        %{id: "goal-missing-integration", state: "active", current_revision: 1},
        related
      )

    assert [%{reason: "integration_outcome_required", ids: ["integration-item"]} = blocker] =
             projection.blockers

    assert blocker.details == [
             %{work_item_id: "integration-item", reason: "accepted_outcome_required"}
           ]

    refute projection.completion.ready?
    refute "achieve" in projection.allowed_actions
    refute "final_acceptance_required" in projection.blocker_reasons

    goal =
      struct(Goal, %{
        id: "goal-missing-integration",
        project_id: "project-missing-integration",
        title: "Missing integration outcome",
        state: "active",
        current_revision: 1,
        lock_version: 1,
        updated_at: ~U[2026-09-10 00:00:00Z],
        revisions: related.revisions,
        work_items: related.work_items,
        dependencies: [],
        outcomes: related.outcomes,
        decisions: related.decisions
      })

    Process.put(:goals_read_model_attention_rows, [goal])
    on_exit(fn -> Process.delete(:goals_read_model_attention_rows) end)

    assert {:ok, %{entries: [entry]}} = ReadModel.attention(repo: CursorRepo, limit: 1)
    assert entry.goal_id == goal.id
    assert entry.blocker_reasons == ["integration_outcome_required"]
    refute entry.ready_to_achieve?

    rejected =
      ReadModel.project(
        %{id: "goal-missing-integration", state: "active", current_revision: 1},
        Map.put(related, :outcomes, [
          %{
            id: "rejected-integration-outcome",
            goal_revision: 1,
            work_item_id: "integration-item",
            disposition: "rejected",
            subject_hash: subject_hash
          }
        ])
      )

    assert "integration_outcome_required" in rejected.blocker_reasons
    refute rejected.completion.ready?
  end

  test "authority policy keeps deterministic execution behind an exact completion Decision" do
    subject = subject()
    subject_hash = SymmetryControl.RequestHash.canonical(subject)

    related =
      final_acceptance_related(subject, subject_hash,
        decisions: [],
        acceptance_contract: %{
          "predicates" => [%{"id" => "operator", "kind" => "operator_acceptance"}]
        }
      )
      |> put_in([:revisions, Access.at(0), :authority_policy], %{
        "operator_required_for_completion" => true
      })
      |> put_in([:revisions, Access.at(0), :execution_policy], %{
        "final_acceptance" => "deterministic"
      })

    projection =
      ReadModel.project(%{id: "goal-final-ready", state: "active", current_revision: 1}, related)

    refute projection.completion.ready?

    assert [%{reason: "final_acceptance_required", ids: ["integration-item"]}] =
             projection.blockers
  end

  test "one authorized integration subject does not leave another candidate as a final-acceptance blocker" do
    subject_a = subject()
    subject_b = Map.put(subject(), "commit", String.duplicate("d", 40))
    hash_a = SymmetryControl.RequestHash.canonical(subject_a)
    hash_b = SymmetryControl.RequestHash.canonical(subject_b)

    related =
      final_acceptance_related(subject_a, hash_a,
        work_items: [
          %{id: "integration-a", required: false, integration: true, admitted_revision: 1},
          %{id: "integration-b", required: false, integration: true, admitted_revision: 1}
        ],
        outcomes: [
          %{
            id: "outcome-a",
            goal_revision: 1,
            work_item_id: "integration-a",
            disposition: "accepted",
            subject_hash: hash_a
          },
          %{
            id: "outcome-b",
            goal_revision: 1,
            work_item_id: "integration-b",
            disposition: "accepted",
            subject_hash: hash_b
          }
        ],
        decisions: [completion_decision("goal-final-ready", hash_a)]
      )

    projection =
      ReadModel.project(%{id: "goal-final-ready", state: "active", current_revision: 1}, related)

    assert projection.completion.ready?
    assert "achieve" in projection.allowed_actions
    refute "final_acceptance_required" in projection.blocker_reasons
  end

  test "accounting keeps held reservations separate from known usage leaves" do
    projection =
      ReadModel.project(
        %{id: "goal-budget", state: "active", current_revision: 1},
        %{
          revisions: [
            %{
              revision: 1,
              execution_policy: %{"budget_limit_microusd" => 50, "budget_mode" => "soft"}
            }
          ],
          tasks: [%{id: "task-1", goal_id: "goal-budget", goal_revision: 1, state: "completed"}],
          runs: [%{id: "run-1", task_id: "task-1", generation: 1, state: "completed"}],
          reservations: [%{task_id: "task-1", state: "held", reserved_microusd: 42}],
          usage: [
            %{
              id: "usage-1",
              run_id: "run-1",
              usage_key: "turn-1",
              cost_microusd: 10
            }
          ]
        }
      )

    assert projection.revision.execution_policy["budget_limit_microusd"] == "50"
    assert projection.accounting.held_microusd == "42"
    assert projection.accounting.known_usage_cost_microusd == "10"
    assert projection.accounting.usage_cost_microusd == "10"
    refute projection.accounting.usage_cost_unknown?
    assert projection.accounting.unknown_usage_count == 0
    refute "budget_blocked" in projection.blocker_reasons
  end

  test "projects Microusd values as decimal strings without JavaScript precision loss" do
    budget = 9_007_199_254_740_992

    projection =
      ReadModel.project(
        %{id: "goal-large-budget", state: "active", current_revision: 1},
        %{
          revisions: [
            %{
              revision: 1,
              execution_policy: %{
                "budget_limit_microusd" => budget,
                "budget_mode" => "soft"
              }
            }
          ],
          tasks: [
            %{id: "task-1", goal_id: "goal-large-budget", goal_revision: 1, state: "completed"}
          ],
          runs: [%{id: "run-1", task_id: "task-1", generation: 1, state: "completed"}],
          reservations: [%{task_id: "task-1", state: "held", reserved_microusd: budget}],
          usage: [%{id: "usage-1", run_id: "run-1", usage_key: "turn-1", cost_microusd: 0}]
        }
      )

    assert projection.revision.execution_policy["budget_limit_microusd"] == "9007199254740992"
    assert projection.accounting.held_microusd == "9007199254740992"
    assert projection.accounting.known_usage_cost_microusd == "0"
    assert projection.accounting.usage_cost_microusd == "0"

    budget_blocker = Enum.find(projection.blockers, &(&1.reason == "budget_blocked"))

    assert budget_blocker.details == [
             %{
               limit_microusd: "9007199254740992",
               reserved_microusd: "9007199254740992",
               spent_microusd: "0",
               committed_microusd: "9007199254740992",
               unknown_reservation_count: 0,
               unknown_usage?: false
             }
           ]
  end

  test "terminal Goal runs with unknown or missing usage never render a zero total" do
    unknown_leaf =
      ReadModel.project(
        %{id: "goal-usage-unknown", state: "active", current_revision: 1},
        %{
          revisions: [%{revision: 1, execution_policy: %{"budget_limit_microusd" => nil}}],
          tasks: [
            %{id: "task-1", goal_id: "goal-usage-unknown", goal_revision: 1, state: "completed"}
          ],
          runs: [%{id: "run-1", task_id: "task-1", generation: 1, state: "completed"}],
          usage: [%{id: "usage-unknown", run_id: "run-1", usage_key: "turn", cost_microusd: nil}]
        }
      )

    assert unknown_leaf.revision.execution_policy["budget_limit_microusd"] == nil
    assert unknown_leaf.accounting.known_usage_cost_microusd == "0"
    assert unknown_leaf.accounting.usage_cost_microusd == nil
    assert unknown_leaf.accounting.usage_cost_unknown?
    assert unknown_leaf.accounting.unknown_usage_count == 1

    missing_leaf =
      ReadModel.project(
        %{id: "goal-usage-missing", state: "active", current_revision: 1},
        %{
          revisions: [%{revision: 1, execution_policy: %{}}],
          tasks: [
            %{id: "task-1", goal_id: "goal-usage-missing", goal_revision: 1, state: "completed"}
          ],
          runs: [%{id: "run-1", task_id: "task-1", generation: 1, state: "completed"}]
        }
      )

    assert missing_leaf.accounting.known_usage_cost_microusd == "0"
    assert missing_leaf.accounting.usage_cost_microusd == nil
    assert missing_leaf.accounting.usage_cost_unknown?
    assert missing_leaf.accounting.unknown_usage_count == 1
  end

  test "an expired retrying Run without usage blocks strict budget accounting" do
    projection =
      ReadModel.project(
        %{id: "goal-expired-usage", state: "active", current_revision: 1},
        %{
          revisions: [
            %{
              revision: 1,
              execution_policy: %{
                "budget_limit_microusd" => 100,
                "budget_mode" => "strict"
              }
            }
          ],
          tasks: [
            %{
              id: "task-1",
              goal_id: "goal-expired-usage",
              goal_revision: 1,
              state: "queued"
            }
          ],
          runs: [%{id: "run-1", task_id: "task-1", generation: 1, state: "expired"}],
          reservations: [%{task_id: "task-1", state: "held", reserved_microusd: 10}]
        }
      )

    assert projection.accounting.known_usage_cost_microusd == "0"
    assert projection.accounting.usage_cost_microusd == nil
    assert projection.accounting.usage_cost_unknown?
    assert projection.accounting.unknown_usage_count == 1

    budget_blocker = Enum.find(projection.blockers, &(&1.reason == "budget_blocked"))

    assert budget_blocker.details == [
             %{
               limit_microusd: "100",
               reserved_microusd: "10",
               spent_microusd: "0",
               committed_microusd: "10",
               unknown_reservation_count: 0,
               unknown_usage?: true
             }
           ]
  end

  test "a correction leaf replaces the superseded unknown usage in accounting" do
    projection =
      ReadModel.project(
        %{id: "goal-usage-correction", state: "active", current_revision: 1},
        %{
          revisions: [%{revision: 1, execution_policy: %{}}],
          tasks: [
            %{
              id: "task-1",
              goal_id: "goal-usage-correction",
              goal_revision: 1,
              state: "completed"
            }
          ],
          runs: [%{id: "run-1", task_id: "task-1", generation: 1, state: "completed"}],
          usage: [
            %{id: "usage-unknown", run_id: "run-1", usage_key: "partial", cost_microusd: nil},
            %{
              id: "usage-correction",
              run_id: "run-1",
              usage_key: "final",
              cost_microusd: 10,
              supersedes_id: "usage-unknown"
            }
          ]
        }
      )

    assert projection.accounting.usage_records == 2
    assert projection.accounting.usage_leaf_count == 1
    assert projection.accounting.known_usage_cost_microusd == "10"
    assert projection.accounting.usage_cost_microusd == "10"
    refute projection.accounting.usage_cost_unknown?
    assert projection.accounting.unknown_usage_count == 0
  end

  test "context sanitization removes credentials and raw transcript fields" do
    sanitized =
      ReadModel.sanitize_context(%{
        "summary" => "safe",
        "provider_token" => "do-not-return",
        "nested" => %{"password" => "do-not-return", "value" => "kept"},
        "raw_transcript" => "do-not-return"
      })

    assert sanitized["summary"] == "safe"
    assert sanitized["nested"]["value"] == "kept"
    refute Map.has_key?(sanitized, "provider_token")
    refute Map.has_key?(sanitized, "raw_transcript")
    refute inspect(sanitized) =~ "do-not-return"
  end

  test "attention cursor carries timestamp and id across ties and timestamp changes" do
    same_time = ~U[2026-09-09 01:00:00.123456Z]
    older_time = ~U[2026-09-09 00:00:00.123456Z]
    oldest_time = ~U[2026-09-08 00:00:00.123456Z]

    goals = [
      %Goal{id: "goal-2", state: "paused", current_revision: 1, updated_at: same_time},
      %Goal{id: "goal-1", state: "paused", current_revision: 1, updated_at: same_time},
      %Goal{id: "goal-0", state: "paused", current_revision: 1, updated_at: older_time},
      %Goal{id: "goal-oldest", state: "paused", current_revision: 1, updated_at: oldest_time}
    ]

    Process.put(:goals_read_model_attention_rows, goals)

    on_exit(fn -> Process.delete(:goals_read_model_attention_rows) end)

    assert {:ok,
            %{
              entries: [%{goal_id: "goal-2"}],
              next_cursor: first_cursor,
              next_after: next_after
            }} =
             ReadModel.attention(repo: CursorRepo, limit: 1)

    assert next_after == first_cursor

    assert decode_attention_cursor(first_cursor) == %{
             "id" => "goal-2",
             "updated_at" => DateTime.to_iso8601(same_time)
           }

    Process.put(:goals_read_model_attention_rows, Enum.drop(goals, 1))

    assert {:ok, %{entries: [%{goal_id: "goal-1"}], next_cursor: second_cursor}} =
             ReadModel.attention(repo: CursorRepo, limit: 1, cursor: first_cursor)

    assert decode_attention_cursor(second_cursor) == %{
             "id" => "goal-1",
             "updated_at" => DateTime.to_iso8601(same_time)
           }

    assert_receive {:attention_goal_query, first_query}
    assert_receive {:attention_goal_query, second_query}
    assert first_query.wheres == []
    assert inspect(second_query.wheres) =~ "updated_at"
    assert inspect(second_query.wheres) =~ "id"

    Process.put(:goals_read_model_attention_rows, Enum.drop(goals, 2))

    assert {:ok, %{entries: [%{goal_id: "goal-0"}], next_cursor: third_cursor}} =
             ReadModel.attention(repo: CursorRepo, limit: 1, cursor: second_cursor)

    assert decode_attention_cursor(third_cursor) == %{
             "id" => "goal-0",
             "updated_at" => DateTime.to_iso8601(older_time)
           }

    Process.put(:goals_read_model_attention_rows, [List.last(goals)])

    assert {:ok, %{entries: [%{goal_id: "goal-oldest"}], next_cursor: nil}} =
             ReadModel.attention(repo: CursorRepo, limit: 1, cursor: third_cursor)
  end

  test "context sanitization rejects case, camelCase, underscore, and hyphen variants" do
    sanitized =
      ReadModel.sanitize_context(%{
        "privateKey" => "private-key-secret",
        "PRIVATE-KEY" => "private-key-secret",
        "sessionFilename" => "local-session-secret",
        "localHandle" => "local-handle-secret",
        "rawTranscript" => "raw-transcript-secret",
        "API-key" => "api-key-secret",
        "nested" => %{"Api_Key" => "nested-api-key-secret"},
        "safe_field" => "kept"
      })

    assert sanitized["safe_field"] == "kept"
    assert sanitized["nested"] == %{}

    for key <- [
          "privateKey",
          "PRIVATE-KEY",
          "sessionFilename",
          "localHandle",
          "rawTranscript",
          "API-key"
        ] do
      refute Map.has_key?(sanitized, key)
    end

    refute inspect(sanitized) =~ "secret"
  end

  defp decode_attention_cursor(cursor) do
    {:ok, payload} = Base.url_decode64(cursor, padding: false)
    Jason.decode!(payload)
  end

  defp settled_projection(settlement, opts \\ []) do
    goal_id = "goal-settlement"
    current_revision = Keyword.get(opts, :current_revision, 1)
    receipt_revision = Keyword.get(opts, :receipt_revision, current_revision)
    task = Keyword.get(opts, :task, settled_task(goal_id, receipt_revision))
    run = Keyword.get(opts, :run, settled_run(task.id, task.current_generation))

    work_items =
      Keyword.get(opts, :work_items, [
        %{
          id: "item-1",
          required: Keyword.get(opts, :required?, true),
          admitted_revision: current_revision,
          orchestration_task_id: task.id
        }
      ])

    response = %{
      "goal_id" => goal_id,
      "goal_revision" => receipt_revision,
      "task_id" => task.id,
      "run_id" => run.id,
      "generation" => run.generation,
      "settlement" => settlement,
      "result_id" => "result-1",
      "result_kind" => "progress",
      "reason" => Keyword.get(opts, :reason),
      "blocker" => Keyword.get(opts, :blocker),
      "proposed_next_action" => Keyword.get(opts, :proposed_next_action),
      "proposed_next_action_status" => Keyword.get(opts, :proposed_next_action_status),
      "subject_hash" => "sha256:" <> String.duplicate("c", 64),
      "evidence_refs" => []
    }

    event = %{
      id: "settlement-event-1",
      sequence: 1,
      kind: "task_settled",
      revision: receipt_revision,
      payload: %{
        "task_id" => task.id,
        "run_id" => run.id,
        "generation" => run.generation,
        "state" => run.state,
        "goal_revision" => receipt_revision
      },
      response: response
    }

    task_settled_events =
      if Keyword.get(opts, :replayed?, false),
        do: [event, %{event | id: "settlement-event-replay", sequence: 2}],
        else: [event]

    ReadModel.project(
      %{
        id: goal_id,
        state: Keyword.get(opts, :state, "active"),
        current_revision: current_revision
      },
      %{
        revisions: [%{revision: current_revision, execution_policy: %{}}],
        work_items: work_items,
        outcomes: Keyword.get(opts, :outcomes, []),
        decisions: Keyword.get(opts, :decisions, []),
        tasks: Keyword.get(opts, :tasks, [task]),
        runs: Keyword.get(opts, :runs, [run]),
        task_settled_events: task_settled_events
      }
    )
  end

  defp settled_task(goal_id \\ "goal-settlement", revision \\ 1) do
    %{
      id: "task-1",
      goal_id: goal_id,
      work_item_id: "item-1",
      goal_revision: revision,
      current_generation: 1,
      state: "completed"
    }
  end

  defp settled_run(task_id, generation) do
    %{id: "run-1", task_id: task_id, generation: generation, state: "completed"}
  end

  defp final_acceptance_related(_subject, subject_hash, opts \\ []) do
    %{
      revisions: [
        %{
          revision: 1,
          authority_policy:
            Keyword.get(opts, :authority_policy, %{
              "operator_required_for_completion" => true
            }),
          execution_policy: %{"final_acceptance" => "operator"},
          acceptance_contract:
            Keyword.get(opts, :acceptance_contract, %{
              "predicates" => [%{"id" => "operator", "kind" => "operator_acceptance"}]
            })
        }
      ],
      work_items:
        Keyword.get(opts, :work_items, [
          %{id: "integration-item", required: false, integration: true, admitted_revision: 1}
        ]),
      outcomes:
        Keyword.get(opts, :outcomes, [
          %{
            id: "integration-outcome",
            goal_revision: 1,
            work_item_id: "integration-item",
            disposition: "accepted",
            subject_hash: subject_hash
          }
        ]),
      decisions:
        Keyword.get(opts, :decisions, [completion_decision("goal-final-ready", subject_hash)]),
      tasks: Keyword.get(opts, :tasks, []),
      runs: Keyword.get(opts, :runs, []),
      evidence: Keyword.get(opts, :evidence, [])
    }
  end

  defp completion_decision(goal_id, subject_hash) do
    %{
      id: "completion-decision",
      goal_revision: 1,
      kind: "completion",
      state: "resolved",
      subject_hash: subject_hash,
      resolution: %{"option_id" => "accept"},
      action_hash:
        SymmetryControl.RequestHash.canonical(%{
          kind: "completion",
          goal_id: goal_id,
          revision: 1,
          subject_hash: Base.encode16(subject_hash, case: :lower)
        })
    }
  end

  defp goal_check_evidence(subject, subject_hash) do
    wire_subject_hash = "sha256:" <> Base.encode16(subject_hash, case: :lower)

    %{
      id: "goal-check-evidence",
      run_id: "validation-run",
      kind: "check",
      subject_hash: subject_hash,
      validator_profile: "test",
      verdict: "passed",
      payload: %{
        "predicate_id" => "goal-check",
        "subject" => subject,
        "subject_hash" => wire_subject_hash
      },
      source_ref: %{
        "subject_hash" => wire_subject_hash,
        "validator_profile" => "test"
      }
    }
  end

  defp task_result(kind, subject, opts) do
    %{
      "schema_version" => "symmetry.task_result.v1",
      "result_id" => "11111111-1111-4111-8111-111111111111",
      "kind" => kind,
      "summary" => "Waiting for an external check.",
      "subject" => subject,
      "subject_hash" => subject_hash(subject),
      "evidence_refs" => [],
      "blocker" => Keyword.get(opts, :blocker),
      "proposed_next_action" => nil,
      "reason" => nil,
      "diagnostics" => []
    }
  end

  defp subject do
    %{
      "resource_id" => "00000000-0000-4000-8000-000000000001",
      "commit" => String.duplicate("a", 40),
      "tree_digest" => "sha256:" <> String.duplicate("b", 64)
    }
  end

  defp subject_hash(subject) do
    "sha256:" <>
      (subject
       |> SymmetryControl.RequestHash.canonical()
       |> Base.encode16(case: :lower))
  end
end
