# terminal-share

Share what a home PC's terminal window shows as a public web page.

Picking "Share this window" in the at menu captures the window the menu is open over
(every pane, in its layout) with `term-share` (romaine-life/at-home-tools) and posts it
here. The service stores the snapshot under an unguessable ID and answers with a link,
`https://share.romaine.life/s/<id>`, that anyone can open for 30 days. The page draws the
snapshot with Rio's terminal engine compiled for the browser, in the terminal's own font
and colors.

Founding work package: [TSH-1](https://openproject.home.romaine.life/wp/TSH-1).

The service, its page, how it is deployed and how to test it arrive with the first PR.
