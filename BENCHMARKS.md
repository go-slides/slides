# Benchmarks

go-slides against [marp-cli](https://github.com/marp-team/marp-cli), the
tool whose output it reproduces, on the same decks and the same machine.
Every figure below comes from `internal/cmd/slidesbench` and the Go
benchmarks in this repository; the commands to reproduce them are at the end.

## Editable PPTX

`--pptx` now writes editable slides — text boxes in the deck's embedded
fonts, shapes, gradients, pictures stored once (go-webengine `officecanvas`,
go-ooxml v0.5.0) — where marp-cli's PPTX, and v0.4.0's, is one picture a
slide. Measured on 2026-10-19 (`slidesbench -modes pptx`, 5 interleaved
rounds, median [range]); the load average was 7.1 at the start but **9–10
during the rounds** (other sessions on the machine), so the times are high
and their ranges wide; the sizes and the ratios are what to read:

| Deck | go-slides (editable) | marp-cli (pictures) | v0.4.0 (pictures) | sizes: go-slides / marp-cli / v0.4.0 | memory: go-slides / marp-cli / v0.4.0 |
| --- | ---: | ---: | ---: | ---: | ---: |
| 100 slides | **484 ms** | 8,159 ms | 4,661 ms | **760 KiB** / 7.2 MiB / 4.8 MiB | **206** / 1,386 / 540 MiB |
| gaia, 20 | **118 ms** | 4,686 ms | 1,403 ms | **893 KiB** / 4.8 MiB / 965 KiB | **124** / 1,349 / 240 MiB |
| math and code, 20 | **242 ms** | 3,017 ms | 808 ms | **842 KiB** / 1.5 MiB / 1.5 MiB | **149** / 1,379 / 270 MiB |

CPU time of the 100-slide deck: 1.4 s against marp-cli's 8.3 s and v0.4.0's
15.7 s (which PNG-encoded 100 slides at 2560×1440). Most of an editable
deck's size is its fonts, embedded whole so that new text can be typed in
them (gaia's Lato and Roboto Mono faces); a one-slide oracle deck in Inter
is already 480 KiB, so the slides themselves are a few KiB each. The ODP is the same drawing (570 KiB for the 100 slides,
`officecheck`, in-process).

**Same layout as the image export.** `go run ./internal/cmd/officecheck --out
DIR` (DIR outside the repository) converts the editable PPTX and ODP to PDF
with LibreOffice 25.8, renders the pages with poppler and compares them with
`export.Rasterize`: 1.7 % (PPTX) and 1.4 % (ODP) of the pixels differ by more
than 48 on the 100-slide deck, 0.04–1.9 % on the oracle decks, on glyph edges
(another rasteriser). LibreOffice renders them in the embedded Lato and Inter
the machine does not have; `pdftotext` reads 2,240 words back.

## Vector PDF

`--pdf` now draws each slide as vectors — text in the embedded fonts the
slide was laid out with, backgrounds, borders and gradients as paths and
shadings, each image once (go-webengine `paint.PaintVector` through
`pdfcanvas`, go-pdfkit v0.15.0). Measured on 2026-10-19 on the same machine
(load average 3.1 at the start), `slidesbench -modes pdf`, against marp-cli
4.5.1 and the image PDF of v0.3.0 ("before"); median of 5 interleaved rounds,
memory the whole process tree:

| Deck | go-slides | marp-cli | before | go-slides size | marp-cli size | before size | go-slides memory | marp-cli memory | before memory |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 100 slides | **306 ms** | 1,763 ms | 1,147 ms | **758 KiB** | 837 KiB | 10.0 MiB | **161 MiB** | 1,509 MiB | 243 MiB |
| gaia, 20 | **93 ms** | 1,546 ms | 285 ms | **32 KiB** | 66 KiB | 582 KiB | **134 MiB** | 1,320 MiB | 156 MiB |
| math and code, 20 | **140 ms** | 1,643 ms | 294 ms | **124 KiB** | 335 KiB | 638 KiB | **153 MiB** | 1,452 MiB | 164 MiB |

The CPU time of the 100-slide PDF falls from 3.3 s to 0.9 s (marp-cli: 2.7 s):
painting into a PDF is cheaper than painting pixels and compressing them. On
the seven oracle decks the PDF takes 80–93 ms against marp-cli's 1.37–1.47 s
and is 1 KiB–14 KiB against 1.1–66 KiB.

**Same layout as the image export.** Both are painted from one layout pass.
`go run ./internal/cmd/pdfcheck -out DIR` (DIR outside the repository) renders
every page with poppler's `pdftoppm` at 96 dpi and compares it with
`export.Rasterize`: on the 100-slide deck 0.46 % of the pixels differ by more
than 48 in a channel (worst slide 1.2 %), all on glyph edges — the PDF places
glyphs at the layout's exact advances where the raster rounds them to whole
pixels. `pdftotext` reads the text back (2,240 words for the 100 slides).

What is still drawn as a bitmap, per element and only where it occurs: a
subtree under `opacity` < 1, `filter`, `mask-image` or a rotation, a blurred
`box-shadow`, a gradient whose opacity fades, and formulas (go-tex's SVG is
rasterised by the engine). The text under such an element is laid over its
bitmap invisibly, so it can still be selected. `--pdf-raster` writes the PDF
as before, one image per page.

## Results

The table below is from before the vector PDF; its PDF rows are the image
PDF.

Measured on 2026-10-19 on an Apple M4 Max (16 CPUs, macOS 26.6.2, load
average 7.3 at the start), go1.27.2, GOMAXPROCS unset (16): go-slides at
b439aff (go-webengine v0.8.5, go-pdfkit v0.14.1), marp-cli 4.5.1 (Marp Core
4.4.0) with Google Chrome 155.0.8059.84, and go-slides before this change
(main at 975cda3) as "before". Median of 5 interleaved rounds; memory is the
peak of the whole process tree (see Method).

| Deck | Mode | go-slides | marp-cli | before | go-slides memory | marp-cli memory | before memory |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 100 slides | HTML | **36 ms** | 315 ms | 36 ms | **47 MiB** | 140 MiB | 47 MiB |
| 100 slides | PDF | **1.14 s** | 1.88 s | 27.0 s | **242 MiB** | 1,509 MiB | 923 MiB |
| 100 slides | PNG | **0.97 s** | 5.44 s | 26.4 s | **225 MiB** | 1,354 MiB | 853 MiB |
| 100 slides | PPTX | **3.39 s** | 8.03 s | 34.1 s | **530 MiB** | 1,373 MiB | 2,744 MiB |
| gaia, 20 | HTML | **24 ms** | 296 ms | 25 ms | **34 MiB** | 134 MiB | 34 MiB |
| gaia, 20 | PDF | **283 ms** | 1.57 s | 5.65 s | **154 MiB** | 1,315 MiB | 307 MiB |
| gaia, 20 | PNG | **305 ms** | 2.50 s | 5.62 s | **151 MiB** | 1,248 MiB | 306 MiB |
| gaia, 20 | PPTX | **993 ms** | 3.57 s | 8.26 s | **241 MiB** | 1,352 MiB | 652 MiB |
| math and code, 20 | HTML | **35 ms** | 408 ms | 36 ms | **52 MiB** | 150 MiB | 52 MiB |
| math and code, 20 | PDF | **290 ms** | 1.71 s | 5.48 s | **161 MiB** | 1,461 MiB | 321 MiB |
| math and code, 20 | PNG | **250 ms** | 2.32 s | 5.36 s | **168 MiB** | 1,286 MiB | 312 MiB |
| math and code, 20 | PPTX | **625 ms** | 2.76 s | 6.72 s | **246 MiB** | 1,315 MiB | 642 MiB |

On the seven oracle decks (1 to 8 slides) go-slides takes 24–27 ms for HTML
against marp-cli's 225–317 ms, and 96–238 ms for PDF, PNG and PPTX against
1.35–1.82 s, in 87–185 MiB against 1.17–1.42 GiB.

What go-slides does **not** win:

- **PDF size** — no longer: see [Vector PDF](#vector-pdf). The image PDF
  measured here was 10.0 MiB against 837 KiB for 100 slides, gaia's 582 KiB
  against 66 KiB.
- **CPU time.** Rendering on four workers, go-slides spends more CPU than
  wall time: 3.2 s of CPU for the 100-slide PDF against marp-cli's 2.7 s, and
  12.0 s against 7.9 s for its PPTX (PNG-encoding 100 slides at 2560×1440). On
  the 20-slide decks it spends less than marp-cli.
- **The memory comparison** is an upper bound for marp-cli: Chrome's
  processes share pages, which the tree sum counts in each. Its largest single
  process is 220–320 MiB, still above go-slides' whole process on every deck
  and mode but two PPTX: 530 against 272 MiB for 100 slides, 246 against
  239 MiB for the math deck — go-slides keeps the 2560×1440 PNGs until the
  file is written.
- **HTML math** is larger: 891 KiB against 587 KiB for the math deck (go-tex
  writes each formula as SVG).

Before this change go-slides was **slower than marp-cli** on every image,
PDF and PPTX export but the smallest decks: 27 s against 1.9 s for the
100-slide PDF.

<details><summary>Every deck, mode and tool: wall and CPU time, tree peak and largest process, output</summary>

| Deck | Mode | Tool | Wall (ms) | CPU (ms) | Tree peak (MiB) | Largest process (MiB) | Output |
|---|---|---|---:|---:|---:|---:|---:|
| generated-100 | html | go-slides | 36 [35–40] | 65 | 47 [46–48] | 47 | 268.8 KiB |
| generated-100 | html | marp | 315 [313–343] | 461 | 140 [140–149] | 140 | 316.3 KiB |
| generated-100 | html | go-slides@base | 36 [36–38] | 62 | 47 [47–48] | 47 | 268.8 KiB |
| generated-100 | pdf | go-slides | 1144 [1141–1147] | 3241 | 242 [219–265] | 242 | 10.0 MiB |
| generated-100 | pdf | marp | 1877 [1826–1962] | 2723 | 1509 [1504–1510] | 321 | 837.2 KiB |
| generated-100 | pdf | go-slides@base | 27032 [26915–32857] | 31299 | 923 [848–940] | 923 | 10.0 MiB |
| generated-100 | png | go-slides | 966 [935–1024] | 3664 | 225 [218–233] | 225 | 3.5 MiB in 100 files |
| generated-100 | png | marp | 5437 [5415–5473] | 4260 | 1354 [1287–1356] | 234 | 3.1 MiB in 100 files |
| generated-100 | png | go-slides@base | 26409 [26317–30281] | 30529 | 853 [833–857] | 853 | 3.5 MiB in 100 files |
| generated-100 | pptx | go-slides | 3385 [3341–3691] | 11991 | 530 [482–615] | 530 | 4.8 MiB |
| generated-100 | pptx | marp | 8034 [7910–9102] | 7872 | 1373 [1372–1389] | 272 | 7.2 MiB |
| generated-100 | pptx | go-slides@base | 34066 [33833–38060] | 38844 | 2744 [2693–2775] | 2744 | 4.8 MiB |
| gaia-20 | html | go-slides | 24 [24–26] | 33 | 34 [34–35] | 34 | 31.8 KiB |
| gaia-20 | html | marp | 296 [291–315] | 382 | 134 [125–134] | 134 | 85.9 KiB |
| gaia-20 | html | go-slides@base | 25 [25–26] | 33 | 34 [34–35] | 34 | 31.8 KiB |
| gaia-20 | pdf | go-slides | 283 [281–288] | 854 | 154 [150–155] | 154 | 581.5 KiB |
| gaia-20 | pdf | marp | 1571 [1524–1711] | 2286 | 1315 [1314–1319] | 224 | 66.4 KiB |
| gaia-20 | pdf | go-slides@base | 5651 [5645–5663] | 6691 | 307 [305–319] | 307 | 581.5 KiB |
| gaia-20 | png | go-slides | 305 [303–312] | 1036 | 151 [143–155] | 151 | 663.5 KiB in 20 files |
| gaia-20 | png | marp | 2496 [2454–2538] | 2819 | 1248 [1245–1251] | 226 | 1.7 MiB in 20 files |
| gaia-20 | png | go-slides@base | 5619 [5611–5798] | 6670 | 306 [300–313] | 306 | 663.5 KiB in 20 files |
| gaia-20 | pptx | go-slides | 993 [979–1245] | 3649 | 241 [238–258] | 241 | 965.3 KiB |
| gaia-20 | pptx | marp | 3565 [3477–3608] | 4243 | 1352 [1350–1368] | 245 | 4.8 MiB |
| gaia-20 | pptx | go-slides@base | 8261 [8193–8488] | 9062 | 652 [642–668] | 652 | 965.3 KiB |
| math-code-20 | html | go-slides | 35 [34–37] | 58 | 52 [52–53] | 52 | 891.3 KiB |
| math-code-20 | html | marp | 408 [391–415] | 563 | 150 [145–151] | 150 | 586.7 KiB |
| math-code-20 | html | go-slides@base | 36 [35–37] | 57 | 52 [52–53] | 52 | 891.3 KiB |
| math-code-20 | pdf | go-slides | 290 [289–304] | 741 | 161 [158–166] | 161 | 637.8 KiB |
| math-code-20 | pdf | marp | 1713 [1654–1738] | 2626 | 1461 [1450–1464] | 263 | 335.0 KiB |
| math-code-20 | pdf | go-slides@base | 5481 [5461–5496] | 6582 | 321 [306–330] | 321 | 633.1 KiB |
| math-code-20 | png | go-slides | 250 [245–253] | 840 | 168 [159–175] | 168 | 965.1 KiB in 20 files |
| math-code-20 | png | marp | 2319 [2285–2347] | 2686 | 1286 [1278–1287] | 228 | 651.9 KiB in 20 files |
| math-code-20 | png | go-slides@base | 5355 [5344–5363] | 6466 | 312 [306–329] | 312 | 967.0 KiB in 20 files |
| math-code-20 | pptx | go-slides | 625 [622–678] | 2275 | 246 [238–279] | 246 | 1.5 MiB |
| math-code-20 | pptx | marp | 2764 [2698–2778] | 3277 | 1315 [1302–1320] | 239 | 1.5 MiB |
| math-code-20 | pptx | go-slides@base | 6716 [6707–6838] | 7661 | 642 [593–653] | 642 | 1.5 MiB |
| oracle/directives | html | go-slides | 24 [24–24] | 33 | 34 [34–34] | 34 | 21.4 KiB |
| oracle/directives | html | marp | 247 [237–255] | 322 | 121 [112–122] | 121 | 75.9 KiB |
| oracle/directives | html | go-slides@base | 24 [24–25] | 32 | 34 [34–34] | 34 | 21.4 KiB |
| oracle/directives | pdf | go-slides | 119 [117–121] | 194 | 123 [115–124] | 123 | 34.3 KiB |
| oracle/directives | pdf | marp | 1442 [1394–1449] | 2061 | 1285 [1278–1290] | 220 | 13.2 KiB |
| oracle/directives | pdf | go-slides@base | 1094 [1085–1102] | 1353 | 207 [203–213] | 207 | 34.3 KiB |
| oracle/directives | png | go-slides | 110 [108–222] | 198 | 109 [104–110] | 109 | 84.3 KiB in 4 files |
| oracle/directives | png | marp | 1525 [1510–1606] | 2024 | 1202 [1192–1203] | 224 | 39.9 KiB in 4 files |
| oracle/directives | png | go-slides@base | 1057 [1055–1062] | 1307 | 204 [199–212] | 204 | 84.3 KiB in 4 files |
| oracle/directives | pptx | go-slides | 199 [195–526] | 437 | 151 [149–152] | 151 | 53.8 KiB |
| oracle/directives | pptx | marp | 1670 [1579–3302] | 2213 | 1231 [1212–1237] | 235 | 165.8 KiB |
| oracle/directives | pptx | go-slides@base | 1276 [1266–3648] | 1510 | 259 [250–269] | 259 | 53.8 KiB |
| oracle/divider | html | go-slides | 26 [26–57] | 35 | 34 [34–35] | 34 | 53.0 KiB |
| oracle/divider | html | marp | 261 [254–539] | 357 | 133 [120–133] | 133 | 106.6 KiB |
| oracle/divider | html | go-slides@base | 27 [26–65] | 35 | 34 [34–35] | 34 | 53.0 KiB |
| oracle/divider | pdf | go-slides | 112 [110–257] | 173 | 119 [117–123] | 119 | 22.2 KiB |
| oracle/divider | pdf | marp | 1507 [1401–3433] | 2189 | 1397 [1387–1402] | 249 | 8.8 KiB |
| oracle/divider | pdf | go-slides@base | 822 [812–1313] | 1040 | 196 [193–216] | 196 | 22.2 KiB |
| oracle/divider | png | go-slides | 99 [96–206] | 168 | 110 [108–116] | 110 | 55.0 KiB in 3 files |
| oracle/divider | png | marp | 1473 [1432–3017] | 2074 | 1210 [1206–1214] | 223 | 17.1 KiB in 3 files |
| oracle/divider | png | go-slides@base | 792 [787–1246] | 1004 | 198 [196–216] | 198 | 55.0 KiB in 3 files |
| oracle/divider | pptx | go-slides | 134 [132–292] | 280 | 141 [137–146] | 141 | 23.2 KiB |
| oracle/divider | pptx | marp | 1522 [1476–3318] | 2182 | 1230 [1224–1237] | 234 | 102.7 KiB |
| oracle/divider | pptx | go-slides@base | 894 [885–1581] | 1088 | 243 [225–248] | 243 | 23.2 KiB |
| oracle/frontmatter-unclosed | html | go-slides | 24 [23–58] | 32 | 34 [34–34] | 34 | 19.1 KiB |
| oracle/frontmatter-unclosed | html | marp | 225 [218–512] | 301 | 120 [105–121] | 120 | 73.4 KiB |
| oracle/frontmatter-unclosed | html | go-slides@base | 24 [23–52] | 32 | 33 [33–34] | 33 | 19.1 KiB |
| oracle/frontmatter-unclosed | pdf | go-slides | 103 [102–107] | 136 | 91 [88–94] | 91 | 14.9 KiB |
| oracle/frontmatter-unclosed | pdf | marp | 1352 [1330–2108] | 2012 | 1254 [1254–1257] | 221 | 1.1 KiB |
| oracle/frontmatter-unclosed | pdf | go-slides@base | 306 [305–593] | 392 | 157 [157–173] | 157 | 14.9 KiB |
| oracle/frontmatter-unclosed | png | go-slides | 111 [110–113] | 144 | 87 [86–91] | 87 | 19.3 KiB |
| oracle/frontmatter-unclosed | png | marp | 1354 [1281–1387] | 1962 | 1193 [1177–1198] | 223 | 54.0 KiB |
| oracle/frontmatter-unclosed | png | go-slides@base | 303 [302–307] | 382 | 163 [151–170] | 163 | 19.3 KiB |
| oracle/frontmatter-unclosed | pptx | go-slides | 210 [209–211] | 248 | 96 [95–103] | 96 | 22.3 KiB |
| oracle/frontmatter-unclosed | pptx | marp | 1413 [1330–1424] | 2054 | 1216 [1197–1219] | 235 | 208.5 KiB |
| oracle/frontmatter-unclosed | pptx | go-slides@base | 402 [400–403] | 480 | 172 [159–180] | 172 | 22.3 KiB |
| oracle/html | html | go-slides | 26 [25–28] | 34 | 34 [34–34] | 34 | 52.9 KiB |
| oracle/html | html | marp | 267 [245–279] | 373 | 134 [121–136] | 134 | 106.4 KiB |
| oracle/html | html | go-slides@base | 26 [25–28] | 35 | 34 [33–34] | 34 | 52.9 KiB |
| oracle/html | pdf | go-slides | 96 [94–97] | 130 | 96 [96–97] | 96 | 13.1 KiB |
| oracle/html | pdf | marp | 1436 [1364–1526] | 2189 | 1353 [1339–1359] | 221 | 15.2 KiB |
| oracle/html | pdf | go-slides@base | 301 [296–305] | 379 | 165 [161–176] | 165 | 13.1 KiB |
| oracle/html | png | go-slides | 98 [97–100] | 133 | 93 [88–94] | 93 | 25.7 KiB |
| oracle/html | png | marp | 1364 [1328–1385] | 2005 | 1211 [1205–1214] | 222 | 20.4 KiB |
| oracle/html | png | go-slides@base | 289 [288–292] | 367 | 167 [162–176] | 167 | 25.7 KiB |
| oracle/html | pptx | go-slides | 141 [138–143] | 180 | 105 [102–106] | 105 | 29.9 KiB |
| oracle/html | pptx | marp | 1375 [1327–1468] | 2048 | 1232 [1225–1234] | 233 | 90.7 KiB |
| oracle/html | pptx | go-slides@base | 331 [328–333] | 412 | 179 [174–188] | 179 | 29.9 KiB |
| oracle/images | html | go-slides | 26 [25–27] | 35 | 34 [33–34] | 34 | 55.0 KiB |
| oracle/images | html | marp | 249 [245–254] | 351 | 133 [131–135] | 133 | 108.6 KiB |
| oracle/images | html | go-slides@base | 27 [26–27] | 34 | 34 [33–34] | 34 | 55.0 KiB |
| oracle/images | pdf | go-slides | 101 [100–143] | 168 | 129 [128–129] | 129 | 6.7 KiB |
| oracle/images | pdf | marp | 1479 [1415–4144] | 2199 | 1356 [1350–1358] | 222 | 36.0 KiB |
| oracle/images | pdf | go-slides@base | 812 [804–818] | 1021 | 213 [209–231] | 213 | 6.7 KiB |
| oracle/images | png | go-slides | 100 [98–106] | 173 | 121 [120–125] | 121 | 50.2 KiB in 3 files |
| oracle/images | png | marp | 1463 [1416–1491] | 2046 | 1209 [1202–1212] | 224 | 20.7 KiB in 3 files |
| oracle/images | png | go-slides@base | 791 [790–869] | 1011 | 218 [201–228] | 218 | 50.2 KiB in 3 files |
| oracle/images | pptx | go-slides | 144 [142–145] | 282 | 185 [181–188] | 185 | 12.6 KiB |
| oracle/images | pptx | marp | 1514 [1488–4322] | 2129 | 1231 [1224–1293] | 234 | 111.2 KiB |
| oracle/images | pptx | go-slides@base | 898 [888–900] | 1086 | 271 [256–274] | 271 | 12.6 KiB |
| oracle/kinds-expanded | html | go-slides | 27 [26–28] | 36 | 35 [34–36] | 35 | 55.9 KiB |
| oracle/kinds-expanded | html | marp | 285 [273–287] | 385 | 128 [122–129] | 128 | 109.5 KiB |
| oracle/kinds-expanded | html | go-slides@base | 27 [26–28] | 37 | 35 [35–35] | 35 | 55.9 KiB |
| oracle/kinds-expanded | pdf | go-slides | 153 [152–158] | 279 | 145 [142–153] | 145 | 87.3 KiB |
| oracle/kinds-expanded | pdf | marp | 1472 [1421–1517] | 2232 | 1456 [1446–1457] | 302 | 65.7 KiB |
| oracle/kinds-expanded | pdf | go-slides@base | 2143 [2133–2155] | 2616 | 233 [227–243] | 233 | 87.3 KiB |
| oracle/kinds-expanded | png | go-slides | 127 [124–138] | 300 | 133 [130–147] | 133 | 186.5 KiB in 8 files |
| oracle/kinds-expanded | png | marp | 1740 [1690–5395] | 2258 | 1221 [1218–1223] | 225 | 119.6 KiB in 8 files |
| oracle/kinds-expanded | png | go-slides@base | 2080 [2077–2084] | 2559 | 235 [223–256] | 235 | 186.4 KiB in 8 files |
| oracle/kinds-expanded | pptx | go-slides | 238 [236–252] | 680 | 183 [167–196] | 183 | 149.3 KiB |
| oracle/kinds-expanded | pptx | marp | 1820 [1801–1901] | 2388 | 1245 [1238–1247] | 236 | 355.9 KiB |
| oracle/kinds-expanded | pptx | go-slides@base | 2440 [2435–2504] | 2854 | 335 [334–351] | 335 | 149.1 KiB |
| oracle/markdown | html | go-slides | 26 [26–28] | 35 | 34 [34–34] | 34 | 53.7 KiB |
| oracle/markdown | html | marp | 317 [297–330] | 412 | 140 [136–141] | 140 | 107.3 KiB |
| oracle/markdown | html | go-slides@base | 26 [26–29] | 35 | 34 [34–35] | 34 | 53.7 KiB |
| oracle/markdown | pdf | go-slides | 109 [108–120] | 149 | 97 [95–98] | 97 | 25.0 KiB |
| oracle/markdown | pdf | marp | 1542 [1462–1808] | 2373 | 1419 [1401–1425] | 257 | 31.1 KiB |
| oracle/markdown | pdf | go-slides@base | 311 [308–312] | 392 | 164 [157–172] | 164 | 25.0 KiB |
| oracle/markdown | png | go-slides | 112 [109–121] | 147 | 91 [91–95] | 91 | 37.0 KiB |
| oracle/markdown | png | marp | 1444 [1398–1509] | 2099 | 1227 [1215–1231] | 223 | 36.0 KiB |
| oracle/markdown | png | go-slides@base | 302 [301–314] | 387 | 175 [163–177] | 175 | 37.0 KiB |
| oracle/markdown | pptx | go-slides | 193 [191–196] | 231 | 102 [102–106] | 102 | 56.9 KiB |
| oracle/markdown | pptx | marp | 1472 [1432–1500] | 2131 | 1253 [1243–1253] | 234 | 119.1 KiB |
| oracle/markdown | pptx | go-slides@base | 386 [384–390] | 470 | 187 [178–192] | 187 | 57.1 KiB |

</details>

## What was optimised

Profiling the generated deck (`go test -bench Rasterize -cpuprofile
-memprofile ./export/`) found the time in fonts and the memory in holding the
deck. Shares are of the first profile, before any change:

| Change | Where | Measured |
| --- | --- | --- |
| The bundled families (Inter, Lora, Go Mono, DejaVu) were parsed on **every render**, once per slide. Parsed once per process now, shared read-only. | go-webengine [#291](https://github.com/go-webengine/engine/pull/291), v0.8.0 | 18 % of the CPU, 36 % of the allocation |
| Each `@font-face` file was parsed **twice** per render, and nothing was kept between renders. Parsed once, kept in a cache keyed by the SHA-256 of the bytes, bounded to 64 files / 32 MiB. | go-webengine #291 | a page carrying a data: URI face: 50 → 12 ms, 36.6 → 6.2 MB, 73,160 → 1,160 allocations |
| `url(`, `format(` and `!important` were looked for in a **lowercased copy** of the whole value — for an inlined font or image, hundreds of kilobytes copied per declaration. Matched in place. | go-webengine #291 | 16 % of the CPU |
| go-slides inlined its ten Lato and Roboto Mono faces into **every slide** as data: URIs, which the engine tokenised, decoded and parsed per slide. They are installed once with the new `Engine.FontFaces`; the output is byte-identical. | this repository, `export` | a gaia slide: 211 → 6.5 MB allocated |
| `DrawImage` read every pixel through `image.Image.At`, which allocates: 921,600 allocations per 1280×720 page. An `*image.RGBA`'s samples are copied directly. | go-pdfkit [#46](https://github.com/go-pdfkit/pdfkit/pull/46), v0.14.1 | 72.8 → 29.4 ms per page, byte-identical |
| The slides were rendered **one at a time** and the whole deck held as images before anything was written (100 slides: 350 MiB of pixels at scale 1, 1.4 GiB at scale 2 for PPTX). `export.Each` renders on a bounded pool (`Options.Workers`, default min(GOMAXPROCS, 4)) and hands the images over in order, at most `Workers` alive at once; PNG and JPEG are encoded in the workers; `--images`, `--pdf` and `--pptx` write as they go. | this repository, `export` and the CLI | memory bounded by the pool, not by the deck |
| `--image` rendered every slide to keep the first. | the CLI | one slide rendered |

Allocation per 10 slides rendered with one worker (`BenchmarkRasterize`;
allocation counts are exact whatever the load), main at 975cda3 → this
change:

| Deck | Before | After |
| --- | ---: | ---: |
| default theme, data: URI background | 2,294 MB | 180 MB |
| gaia | 2,114 MB | 65 MB |
| math and code | 2,258 MB | 128 MB |

The default pool, `DefaultWorkers` = min(GOMAXPROCS, 4), from
`BenchmarkRasterize/default-10` (5 iterations, load average 6):

| Workers | ms per slide | Peak heap (MiB) |
| ---: | ---: | ---: |
| 1 | 15.6 | 163 |
| 2 | 8.2 | 132 |
| 4 | 6.0 | 144 |
| 8 | 4.7 | 166 |

Each doubling saves less (47 %, 27 %, 21 %); past 4 it costs heap.

## Method

**The tools.** Both run as subprocesses, as a user runs them, writing to a
fresh directory outside the repository:

| Mode | go-slides | marp-cli |
| --- | --- | --- |
| HTML | `slides build --no-config -o deck.html deck.md` | `marp --no-config-file -o deck.html deck.md` |
| PDF | `… --pdf` | `… --pdf --pdf-outlines` (go-slides' PDF carries an outline) |
| PNG | `… --images png` (every slide) | `… --images png` |
| PPTX | `… --pptx` | `… --pptx` |

Images and PDF at scale 1, PPTX at both tools' default, 2. marp-cli reads its
standard input unless it is closed: every tool gets `/dev/null`.

**The decks.** `generated-100`: 100 slides in the default theme, a fifth each
of paragraphs with inline markup, nested lists, fenced code, tables, and a
split background image given as a 640×360 PNG `data:` URI.
`gaia-20`: 20 slides in gaia (web fonts, `lead` and `invert` classes).
`math-code-20`: 20 slides of inline and display math, left to each tool's
default typesetter (MathJax for marp-cli, go-tex/math for go-slides), and
code in four languages. The `oracle/*` decks are `testdata/oracle`, the decks
go-slides' output is checked against. The generators are
`internal/benchdeck`.

**The rounds.** One warm-up round is run and discarded, then 5 measured
rounds. Within a round every deck and mode runs each tool once, one after
the other, and the order is reversed every other round, so that a drift in
the machine's state falls on every tool. The table gives the median, and the
range of the 5 rounds.

**The load.** Numbers taken on a busy machine are meaningless: before each
round the harness reads the 1-minute load average (`sysctl vm.loadavg`) and
waits while it is above 8 (16 CPUs). A first complete run on 2026-10-18
waited two hours for a load of 70–250 to fall, and other sessions loaded
the machine again between its rounds: its ranges are wider and some medians
far off (marp-cli's 100-slide HTML at 1,075 ms against 315 ms here), so it is
not used. The load at each run is in the JSON output.

**Time.** Wall time is from start to exit. CPU time is `ru_utime + ru_stime`
from `wait4` (Go's `ProcessState.SysUsage`), which counts the process and
every descendant it waited for — marp's Chrome included.

**Memory.** getrusage(2) on macOS gives `ru_maxrss` in bytes (Linux: in
kilobytes), and for a waited-for process it is the **largest single process**
among it and its descendants, not their sum: Chrome runs a browser, a GPU
and renderer processes at once, so `ru_maxrss` understates marp. The harness
therefore also samples the whole process tree every 10 ms — the processes
from `sysctl kern.proc.all`, each one's resident size from `proc_info(2)`
(`PROC_PIDTASKINFO`, called directly, no cgo) — and reports the largest sum
it saw, or `ru_maxrss` when that is larger (a process too short-lived for a
sample). A resident set counts pages shared between processes in each of
them, so the tree sum is an **upper bound** for marp; the "largest process"
column is a lower bound. go-slides is one process: both columns are the same
measure.

`slidesbench -calibrate` checks this on processes of known size:

```text
one process, 256 MiB, 500 ms CPU
  expect: maxrss ≈ tree ≈ 256 MiB + runtime; CPU ≈ 500 ms
  read:   wall 1412 ms, CPU 500 ms, ru_maxrss 261.8 MiB, tree peak 261.8 MiB over 1 processes
one process, 64 MiB, no CPU
  expect: maxrss ≈ tree ≈ 64 MiB + runtime; CPU ≈ 0
  read:   wall 319 ms, CPU 12 ms, ru_maxrss 69.6 MiB, tree peak 69.6 MiB over 1 processes
64 MiB parent waiting on a 256 MiB child that burns 500 ms
  expect: maxrss ≈ 256 MiB (the largest one); tree ≈ 64 + 256 MiB (both alive); CPU ≈ 500 ms (the child's included)
  read:   wall 1781 ms, CPU 512 ms, ru_maxrss 261.8 MiB, tree peak 331.4 MiB over 2 processes
```

**Clean-up.** After each run, any descendant still alive is killed by PID,
once its start time shows the PID was not reused. None was left behind.

## Reproduce

```sh
go run ./internal/cmd/slidesbench -calibrate
go build -o /tmp/slides-base ./cmd/slides     # on the commit to compare with
go run ./internal/cmd/slidesbench -baseline /tmp/slides-base -rounds 5 -json results.json
go run ./internal/cmd/slidesbench -modes pdf -baseline /tmp/slides-base -rounds 5
go run ./internal/cmd/pdfcheck -out /tmp/pdfcheck   # vector PDF against the raster render; needs poppler
go run ./internal/cmd/slidesbench -modes pptx -baseline /tmp/slides-base -rounds 5
go run ./internal/cmd/officecheck --out /tmp/officecheck   # editable PPTX/ODP through LibreOffice; needs poppler
go test -run '^$' -bench . -benchmem . ./export/
```
