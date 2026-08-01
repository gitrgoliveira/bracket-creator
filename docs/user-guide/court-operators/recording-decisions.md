# Record match decisions

Not every bout is decided on points. The score editor records the kendo outcomes described here. Open the score editor from the **Scores** tab on a competition page, or from a match card in the court console (see [Score a match](scoring-a-match.md)).

![The score editor's decision controls: Decide by hantei, Kiken (Voluntary and Injury), Fusenpai, and Fusensho (team only).](../../screenshots/mobile-score-editor.png)

## Kiken (withdrawal)

Kiken covers two distinct situations with different consequences.

### Voluntary withdrawal (FIK Article 31)

A voluntary kiken is permanent. The competitor takes no further matches in this competition and cannot be reinstated.

### Injury withdrawal (FIK Article 30)

An injury kiken can be reinstated later by the operator if the competitor recovers and is fit to continue. Until reinstated, the competitor is blocked from starting any further match.

## Fusenpai and fusensho

**Fusenpai** is a no-show default loss: the competitor who did not appear forfeits the match.

**Fusensho** is a per-bout default win, used in team matches when the opposing team fields a vacant position.

## Hikiwake

A hikiwake is a draw. It applies in pool, league, and Swiss matches (not in the knockout phase) and contributes to the standings separately from wins and losses.

## Encho (overtime)

Encho is the extra period played when a knockout match is level at the end of regulation. It follows ippon-shobu rules: the first competitor to score wins.

In the score editor, open the **Overtime** control and tick **Encho started**. A counter appears so you can record how many overtime periods were fought, using the **+** and **-** buttons. The counter starts at 1 and has no upper limit: how many periods are fought, and how a match still level after them is finally settled (a judges' decision for an individual bout, a daihyosen for a team encounter), is the shimpan's call. Record what actually happened on court, however many periods that took.

![The score editor during a second overtime period: the Encho started box is ticked, the period counter reads two, and the header shows the live overtime readout.](../../screenshots/mobile-encho-overtime.png)

Completed results follow the paper score sheet's layout. The centre, between the two sides' points, only ever carries one mark: **vs** while nothing is decided, **X** for a tie, **(E)** for a match that went to overtime, or **(DH)** for a team encounter sent to a representative bout. The marks never combine: a match that went to encho cannot end in a tie, and a daihyosen has no overtime. A match won by men in overtime reads **M (E) ·** on the court console, the public viewer, and the exported results workbook, and the marker is the same however many overtime periods were fought: the counter records the number for the tournament log, but results never show it.

Everything else is a result, written beside the competitor it names: **Ht** next to the winner of a judges' decision, **Kiken** next to the competitor who withdrew, **Fus.** next to a no-show. A match decided by hantei after a tied overtime reads **M Ht (E) K**, with the winner's mark on the winner's side of the centre. A team encounter shows its team totals in the score cell instead, so for those the marks appear on the bracket and in the exported workbook.

![A completed result on the public viewer: the winner's men ippon on the left, the E overtime marker in the centre between the two scores, and the opponent on the right.](../../screenshots/viewer-result-encho.png)

## Daihyosen

A daihyosen is a representative bout used to break a tie when points and ranking criteria cannot separate two teams.

It applies in two situations:

- In the knockout phase, when a team encounter finishes level.
- In team pool or league play, when two teams finish equal on every ranking criterion and the tie decides who advances or how they are seeded.

A tie that does not affect advancement is left as a shared rank with no extra bout. In league play, running the daihyosen is the operator's choice rather than an automatic step; see [Team standings and tie-breaks](../organisers/team-tournaments.md#team-standings-and-tie-breaks) for both options.

The bout is a single-point ippon-shobu with no time limit, between one representative from each tied team; because it runs until someone scores, a daihyosen has no encho. The score editor lets you pick each team's representative from its roster. On the court console, the bout appears with a **(DH)** mark in the centre of the score. In pool and league standings, the team that won its daihyosen carries a **DH** badge.

## Chusen (drawing lots)

Chusen is the last resort when two or more tied teams have played a round of daihyosen and the bouts still do not produce a strict order (for example, a cycle where each team beats another, an all-drawn round, or two teams finishing level on daihyosen wins).

When chusen is required, the **Pools** tab (labelled **League** for league competitions) shows a **"Chusen (drawing lots) required"** panel listing the tied teams. Draw lots offline, enter each team's finishing position in the panel, and record it to settle the order and let the competition advance.

Chusen is the only place ranks are set by hand, and it happens only when the bouts themselves cannot decide the order.

## Competitor eligibility after a decision

A kiken or fusenpai marks the competitor who withdrew or did not appear as ineligible for further matches. The app blocks starting an ineligible competitor, so a withdrawn competitor cannot silently re-enter the draw. An injury kiken (FIK Article 30) can be reversed: once the operator reinstates the competitor, the eligibility block is lifted and they can fight again.
