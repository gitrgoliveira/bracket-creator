5. Should have an Agent skill on how to use the CLI/API

2. Add PDF generator from spreadsheet (with Web UI, etc)
3. add square_prep examples
1. Make brew install work.


4. Schedule also needs an admin view, so we can setup the times per match on pools and playoffs, and get an estimation of the tournament duration. We will need to include breaks in the schedule. 
4. Improve the schedule view.
5. in brackets and pools, it needs to be very clear which side is Red and which side is White. when scoring and for the viewer mode and the schedule view.
6. When displaying the results/scores, it needs to be visible if what points those were, for example: MM-K. Having the number of points is not useful.


* Adding the team line up should be optional. If added, then there should be a button to copy the names to the next match. There is usually someone else doing this work, different from someone doing the score registration.

* In the scoring we can't say which side forfeits or is missing
* In the scoring you can do an impossible results like 2-2


******
* Participants that do not check-in, are not part of the draw, when the competition starts (Start competition button).

* [DONE: mp-j39] you can't seed the reserved slots. We'll enter those manually. Remove the "reserved slots" feature.



* need another password to run the dangerous operations: Reset competition, change participants. And we should be able to change that password in the config.

* When generating the draw, the brackets should also be visible. Viewers will need access to these. The admins will also be able to export the XLSX file.

* A tournament can span multiple days
* By default, the competition date is one of the tournament days.

* schedule estimator is per competition.
* We need to have pool match times, playoff match times, and also take into consideration breaks

* in the viewer UI, the announcements should stagger on top of the UI. the webapp will also need to support browser notifications for announcements.

* As an admin I need a button to create announcement and not go into "Edit tournament details"

* Team order and players can change between each team match


===================================================

* Ability to add Sponsors to the main page 

* Allow UI customization (colours and logo)

===================================================

* Needs a way to host this for free in AWS, GCP and Azure, with the respective Terraform code and instructions

* Excel and PDF generation according to London Cup 2026. Single competition generation and whole tournament PDF generation.

* Replicate LondonCup 2026 with test data

* Replicate LondonCup 2026 with real data

* Replicate LondonCup 2025 with real data

* UI localization files in Portuguese, French, Japanese, and Spanish

===================================================
BUGS
Announcements UI appears twice.
remove Check-in start (HH:MM) / Check-in end (HH:MM)
I want to be able to edit/replace competitors, even after the draw has been generated.

Do not use real club names for examples.

Excel exports need to match the app.

===================================================

Some match rules:
https://www.kendo-guide.com/match-in-kendo-shiai.html



