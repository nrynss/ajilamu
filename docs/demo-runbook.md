# Demo runbook

Record a short video for the entry. The video shows the two things no other tool shows.

- A length bar catching a take that runs too short.
- The ledger reporting what one line cost.

Host: <https://ajilamu.nryn.dev>

Project used below: <https://ajilamu.nryn.dev/d/38fa4e9c69715c3c3b2d011a14d9eccf>

---

## Before you record

1. Open <https://ajilamu.nryn.dev>. The index lists projects under "Your dubs".
2. Use a 16:9 browser window at 100 percent zoom. Hide the bookmarks bar.
3. Open the project page. The header reads "$0.07 spent". Open the Details tab, and the project
   total reads "PROJECT COST $0.06533055". That total is the ledger sum of 65,330,550
   nanodollars recorded in `dev-diary/adversarial-review/charge-wiring-live.md`.
4. Turn the system volume up. Shot 1 plays a take.

---

## Shot 1: the length bar catches a short take

About 30 seconds.

1. Open <https://ajilamu.nryn.dev/d/38fa4e9c69715c3c3b2d011a14d9eccf>.
   You can also click the project card "NASA 75-second clip" marked "In review" and "$0.07".
2. Scroll to the LENGTH CHECKS panel.
3. Point at Line 7 and read its length bar sentence on screen. It says
   `Line 7 length bar. This take is 1.42 seconds short of the slot`.
4. Click "Play take" next to Line 7. The voice stops before the picture does.
5. The LINES NEEDING YOU block at the top names the same line:
   `Line 7 is still 1.42 seconds too short after 1 try, this one needs you`.
6. Say the point out loud. The 2026-09-07 baseline recorded this class of failure as a fit.
   Segment 8 ran 40.9 percent short, and the log called it a fit while 2.9 seconds of dead air
   played over a moving mouth (`dev-diary/observations.md`, Sections 1 and 2.1).

---

## Shot 2: the ledger reports what one line cost

About 30 seconds.

1. Stay on the same project page.
2. In the rail under the video, click the "Lines" tab.
3. Click the number "7" in the line 7 row. The row selects.
4. Click the "Details" tab.
5. Read the heading "What this line cost". Each row is one billed call, and rejected tries carry
   their own rows.
6. Read the line total on screen: `This line has cost $0.00366765 across every try`.
7. Read the project total below it: `PROJECT COST $0.06533055`, followed by `1 segment call, 18
   translation calls, 18 render calls, and 0 agent calls`.
8. Say the point out loud. The project total is the ledger sum. The run wrote 56 rows with 56
   distinct event keys summing to 65,330,550 nanodollars, equal to the run-reported total
   (`dev-diary/adversarial-review/charge-wiring-live.md`).

---

## Optional shot 0: show a live run

About 4 minutes. This spends real money, about $0.07 per dub
(`dev-diary/adversarial-review/t8.2-deploy.md`).

1. Open <https://ajilamu.nryn.dev/new>.
2. Click "Try sample video (NASA 75s clip)". The page reads "Measured fee: $0.023414".
3. Choose "Malayalam (India)" in the Target language picker.
4. Click "Start dubbing". The workspace opens at `/d/<id>` and streams progress with a running
   cost.
5. Expect about four minutes. Measured wall times were 239.44 seconds
   (`charge-wiring-live.md`) and 274 seconds (`t8.2-deploy.md`, run 3).
6. Expect a fee between $0.05688435 and $0.072309 (`t8.1-rerun.md`, `charge-wiring-live.md`,
   `t8.2-deploy.md`).
7. Do not promise the 2026-09-07 line count. The live model may return a different number of
   lines. The 2026-09-09 run returned 7 lines against the baseline 8 (`t8.1-rerun.md`).

---

## Do not claim

- Do not claim the export carries a clean dialogue-only track. The product ducks the film mix
  when no separate music track exists (`t8.1-rerun.md`).
- Do not claim the on-screen text is translated. It is not (`README.md`).
- Do not claim the speaker identity comes from the picture. It comes from voice (`README.md`).
- Do not claim every live run returns 8 lines. It may return fewer (`t8.1-rerun.md`).

---

## Recording tips

- Record the browser window, not the whole desktop.
- Keep the cursor still while a sentence is on screen.
- Capture the click on "Play take" and let the short take finish.
- Keep the video under 3 minutes if the entry form sets a limit.
