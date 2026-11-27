# Print QR-code posters for the venue

Posters let spectators open the live results from their own phone. Print the **viewer home** poster for the entrance and one poster for each shiai-jo, put them on the wall, and anyone who scans a code lands on the right page with no link to type.

Each poster carries a large QR code, the tournament name on a coloured band, a heading, and the address the code opens, printed small underneath so someone can type it if scanning does not work. A shiai-jo poster also shows the court letter in very large type, so it can be read from across the hall.

## Open the posters

Click **Posters** on the dashboard, beside **Backup and restore**. The page works before any competition exists, because the viewer-home poster does not need one. The list at the top of the page holds **Viewer home** and then one poster per shiai-jo, in the order of the tournament's courts.

<figure class="bc-fig" markdown="span">
  ![The Posters page: the list of posters, the choices for the viewer-home poster, and a live preview of it with a logo in the centre of the code and two sponsors along the foot.](../../screenshots/posters-viewer-home.png)
  <figcaption>The Posters page: the choices on the left and the poster as it prints on the right.</figcaption>
</figure>

## Choose what a poster says

Pick a poster from the list. The name of the poster you are editing heads the choices, and the preview beside them changes as you type, before you save anything. A bar above the choices holds **Save**, **Print this poster** and **Print all posters**, and stays in view as you scroll. On a phone the preview comes first, above the choices. Once you scroll down to the choices, the bar names the poster and holds **Save**, **Preview** and **Print**. **Preview** shows the poster over the choices, and **Print** prints that poster. A line under the buttons is kept for naming a poster that is over a limit. **Print all posters** is at the end of the choices on a phone.

- **Heading**: the line above the code. Leave it empty to use the standard words ("Follow the tournament live" on the viewer-home poster, "Follow this court live" on a shiai-jo poster). While the box is empty it shows the standard words in grey, and the line under it says which words print. A heading holds up to 80 characters.
- **Call to action**: an optional second line under the heading, for example "Scan for live results". Leave it empty and nothing prints. It holds up to 120 characters.
- **Over a limit**: a counter under each box shows how many characters it holds. While a heading or call to action is over its limit, its box is outlined in red and **Save** and both print buttons are switched off. The poster's name in the list carries a red **!**, and the line under **Save** names the poster to shorten.
- **Logo**: shown only when the tournament has a logo. Choose **Logo in the centre of the code**, the choice a new poster starts with, or the other place: **Logo above the code** on the viewer-home poster, **Logo in the top corner** on a shiai-jo poster, where the logo prints large in the empty corner beside the letter. The logo prints from the image in the tournament's branding, so use one at least 400 pixels across for a sharp print. The code stays readable with a logo in its centre because the poster uses the code's strongest error correction. A very long address does not leave room for that, so the centre choice is switched off and a sentence under the choices says why. The logo then goes to the other place, and a shorter public address brings the centre choice back.
- **Size**: shown on a shiai-jo poster only. **Large letter**, the choice a new poster starts with, makes the court letter the largest thing on the poster, to read from across the hall. **Larger code** sets the code as large as the viewer-home poster's, about 12 cm wide on A4, and the letter at about half its height, for a wall people scan from further away. The poster keeps the same text, logo and sponsors either way.
- **Show sponsors**: shown only when the tournament has sponsors. Untick it to leave the sponsor strip off this poster.

Each poster keeps its own choices, so the viewer-home poster and the poster for each shiai-jo can say different things. To give every shiai-jo poster the same words, set up one of them and click **Use these choices on every shiaijo**. The button copies that poster's heading, call to action, logo place, size and sponsor choice onto the other shiai-jo posters, and asks first when it would replace what they hold. The viewer-home poster is left as it is, and nothing is saved until you click **Save**.

## Add the logo and the sponsors

The posters use the tournament's own logo and sponsor images. Upload them on **Edit details**, in the **Branding** and **Sponsors** sections (refer to [Branding and sponsors](run-tournament.md#branding-and-sponsors)). The Posters page has an **Open Edit details** button under the choices that takes you there, and the posters pick the images up as soon as you come back.

Sponsors print along the foot of the poster in one row, evenly spaced, up to six of them. The logos grow into the space the rest of the poster leaves, so fewer sponsors, or a poster with a short heading, get larger logos. A shiai-jo poster has less room to spare than the viewer-home poster, because its court letter comes first.

<figure class="bc-fig" markdown="span">
  ![The Posters page showing the poster for shiai-jo A: the court letter in very large type with the logo in the top corner beside it, the code under the heading, and the sponsors along the foot.](../../screenshots/posters-court.png)
  <figcaption>A shiai-jo poster: the court letter is the largest thing on the page, with the logo in the top corner beside it.</figcaption>
</figure>

## Save your choices

Nothing is sent until you click **Save**, which is switched off while there is nothing to save. While you have unsaved changes **Save** is the filled button in the bar; otherwise **Print this poster** is. Each poster with unsaved changes carries a dot beside its name in the list. Once you change something, the bar reads **Unsaved changes** while what you see differs from what is stored, **Saving** while the save is on its way, and **Saved** once it has landed. Saving shares the choices with every device, so the posters look the same wherever you open the page.

- Click **Print this poster** or **Print all posters** at any time, unless a text is over its limit: it prints what is on screen, saved or not. Printing never saves.
- If you leave the page with unsaved changes, the app asks **Leave Posters anyway?**. Choose **Keep entries** to stay and save, or **Discard entries** to leave.
- If another device changed the tournament settings while you were typing, the page says "Not saved: the tournament settings were changed on another device. Your poster text is still here." Your text stays on screen. Click **Save again** to save it over the current settings.
- If the save fails for any other reason, the page says **Not saved** and why, and your text stays on screen.

## What a code opens

The viewer-home poster opens the public viewer home. A shiai-jo poster opens that court's scoreboard display, the same page a TV at the court shows (refer to [Scoreboards and court displays](../spectators/following.md#scoreboards-and-court-displays)). The address under each code shows exactly where it goes, and the page shows it under **The code opens**. Its **Change** button takes you to **Edit details**, where the address is set.

A code never carries a link that reveals competitor names. If a competition [hides competitor names](hide-competitor-names.md), a spectator who scans a poster sees what any other spectator sees.

## Set the public address first

The codes open the tournament's **Public URL**. Set it on **Edit details** (refer to [Tournament details](run-tournament.md#tournament-details-and-the-public-info-page)), using the address people can reach from their own phones.

If no public address is set, the codes open the address the page is open at. When that address is one phones may not reach, such as `localhost`, a venue-network address like `192.168.1.20`, or any address with a port number, the page shows a warning above the poster list. The warning shows on screen only and never prints, and its **Set it in Edit details** button takes you to the tournament details. You can still print: the poster prints the address it would use.

## Print the posters

Click **Print this poster** for the poster in the preview, or **Print all posters** for the viewer-home poster followed by every shiai-jo poster, one per sheet. **Print all posters** asks first: it says how many sheets will print, whether they include changes you have not saved, and which address the codes open. When no public address is set and phones may not reach the address, the question opens with a warning and a **Set the address** button that takes you to **Edit details**. The browser's own print dialog then opens.

The browser's own print command (Cmd+P on a Mac, Ctrl+P elsewhere) prints the same as **Print this poster**: the poster in the preview, on one sheet. While a heading or call to action is over its limit, that sheet says which poster to shorten instead of printing it.

The page keeps this advice under **Printing tips**, closed until you open it:

- **A4 portrait** is the poster's size. Each poster fits one sheet, and no blank sheet is added at the end.
- **A3**: choose A3 paper in the dialog and set the scale to fit the page. The poster scales as a whole, so nothing moves.
- **Colours**: the coloured band prints in colour. You do not need to turn on **Background graphics** in the print dialog.
- To keep a copy as a file, choose **Save as PDF** as the printer.

Print one poster on paper first and scan it with a phone before you print the whole set, to check the logo and the address.

## When the address does not fit a code

A code holds a limited amount of text. A public address too long for any code cannot be drawn, so the poster shows a warning in place of the code, and the address line still prints. A shorter public address in the tournament details fixes it.
