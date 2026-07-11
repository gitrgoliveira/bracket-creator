# Score a match

The court console at `/admin/shiaijo/<court>` (linked from the dashboard under **Shiaijo operator views**) is your primary surface for running bouts at a single court. It shows that court's current and upcoming matches with their match numbers, keeps the scoring flow chained to the same court throughout the session, and prompts you to switch to whichever competition needs the court next.

## Enter scores

Open a match from the Upcoming list to start scoring it; the score editor opens inline within the court console. For outcomes that are not decided on points (withdrawals, no-shows, draws, and representative bouts), see [Record match decisions](recording-decisions.md).

![The score editor: Shiro on the left and Aka on the right, each with ippon buttons (men, kote, do, tsuki), a Mark draw control, foul counters, and an overtime toggle.](../../screenshots/mobile-score-editor.png)

!!! tip
    In self-run events, competitors or table helpers can record their own scores without the admin password. See the [Competitor self-run guide](../competitors/self-run.md) and [Operating modes and access control](../organisers/operating-modes.md) for how that works.

## Send a match back to the queue

If you start the wrong bout, use **Send back to queue** on the running match. The action clears any partial score, removes the match from the active view, and returns it to the Upcoming list so the correct match can start.

!!! note
    Send back to queue only works on a running, unfinished match. A completed, scored match is not affected. To correct a result that has already been recorded, go to the competition view and edit the match there.

## Matches waiting on earlier results

A knockout final cannot be called until the earlier bouts that feed it are scored. While it is still waiting, it appears under a **Later** heading with a **Waiting** tag. Its competitors read "Winner of ..." until the feeding bouts are known.

You cannot start a match that has no confirmed competitors. The Later heading lets you see that more play is scheduled for your court, so the queue never appears empty when further matches are still pending.

## Refresh the court view

If the console looks out of date (for example, a bout that finished on another court has not appeared yet), use **Refresh** in the header to re-pull this court's matches from the server. The console also refreshes automatically when it reconnects after a network drop.

## Run a match before results have synced

At a large event, the bouts feeding a final can run on other courts, and their results may take a moment to reach your console. If you already know who won those earlier bouts, open the waiting match's **Run now** action and record each winner. The match becomes startable immediately without waiting for the official result to arrive.

!!! note
    Recording a winner through **Run now** is provisional. If the official result arrives later and differs, the later result takes over.

## Score without a connection

The court console keeps working if it loses its connection to the server. You can finish scoring the bout in progress, and use **Run now** to resolve and start the next match, all while offline. Everything you enter is saved on the device and sent when the connection returns.

If two courts recorded different results for the same match while one was offline, the more recent change wins when they reconcile.
