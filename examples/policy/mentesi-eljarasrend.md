---
title: Mentési és visszaállítási eljárásrend
author: Informatikai üzemeltetés
date: 2026-09-20
version: "1.2"
classification: Belső használatra
approved-by: Ügyvezető
---

# Mentési és visszaállítási eljárásrend

Ez az eljárásrend azt rögzíti, hogyan készülnek a Példa Kft. rendszereiről
mentések, meddig őrizzük őket, és hogyan állítunk vissza belőlük adatot. Az
*Információbiztonsági szabályzat* része.

## 1. Hatály

Az eljárásrend minden olyan rendszerre vonatkozik, amely a cég üzleti
adatait tárolja: a fájlszerverre, az ügyviteli rendszer adatbázisára, a
levelezésre és a virtuális gépekre. A munkaállomások helyi lemezére nem
terjed ki; azokon üzleti adatot tartósan tárolni nem szabad.

## 2. Fogalmak

Teljes mentés
: A kijelölt adatok egészéről készített másolat.

Növekményes mentés
: Csak az előző mentés óta megváltozott adatokról készített másolat.

Helyreállítási időcél (RTO)
: Az a leghosszabb idő, ameddig egy szolgáltatás kiesése még elfogadható.

Helyreállítási pontcél (RPO)
: Az a legnagyobb adatvesztés időben kifejezve, amely még elfogadható.

## 3. Mentési rend

| Rendszer | Mentés típusa | Gyakoriság | Megőrzés | RTO | RPO |
| --- | --- | --- | ---: | ---: | ---: |
| Ügyviteli adatbázis | teljes + naplók | naponta, naplók 15 percenként | 90 nap | 4 óra | 15 perc |
| Fájlszerver | teljes + növekményes | hetente, illetve naponta | 30 nap | 8 óra | 24 óra |
| Levelezés | növekményes | naponta | 30 nap | 8 óra | 24 óra |
| Virtuális gépek | pillanatkép | hetente | 4 hét | 24 óra | 7 nap |

A mentések a „3-2-1" szabályt követik: **három** példány, **két** különböző
adathordozón, ebből **egy** másik telephelyen.

> [!IMPORTANT]
> A másik telephelyen őrzött példány titkosított. A titkosítási kulcs
> őrzéséért az informatikai vezető felel; a kulcs másolata a páncélszekrényben
> van.

## 4. A mentések ellenőrzése

1. Az üzemeltető minden munkanap reggel átnézi az éjszakai mentések naplóját.
2. Sikertelen mentés esetén:
   - megállapítja az okot, és még aznap megismétli a mentést;
   - ha a mentés két egymást követő napon sikertelen, értesíti az
     informatikai vezetőt.
3. Negyedévente próba-visszaállítást végzünk egy véletlenszerűen kiválasztott
   rendszeren. A próba akkor sikeres, ha a visszaállított adat egyezik az
   eredetivel, és a visszaállítás belefért a helyreállítási időcélba.

## 5. Visszaállítás

A visszaállítást az adatgazda kéri írásban, az informatikai vezető hagyja
jóvá.

```sh
# Az ügyviteli adatbázis visszaállítása egy adott időpontra
pg_restore --clean --dbname=ugyvitel /mentes/ugyvitel-2026-09-19.dump
psql --dbname=ugyvitel --command="SELECT pg_wal_replay_resume();"
```

> [!WARNING]
> Éles rendszerre csak akkor állítunk vissza, ha a jelenlegi állapotról
> előbb mentés készült. Ami egyszer felülíródott, az nem hozható vissza.

A visszaállítás után az adatgazda ellenőrzi, hogy az adatok teljesek-e, és
ezt aláírásával igazolja a visszaállítási naplóban.

## 6. Felelősök

| Feladat | Felelős |
| --- | --- |
| A mentések futtatása és napi ellenőrzése | Üzemeltető |
| A próba-visszaállítások megszervezése | Informatikai vezető |
| A visszaállítás jóváhagyása | Informatikai vezető |
| A visszaállított adatok ellenőrzése | Adatgazda |

## Változástörténet

| Verzió | Dátum | Változás |
| --- | --- | --- |
| 1.0 | 2025-02-10 | Első kiadás |
| 1.1 | 2025-11-03 | A megőrzési idő az ügyviteli adatbázisnál 30-ról 90 napra nőtt |
| 1.2 | 2026-09-20 | Negyedéves próba-visszaállítás bevezetése |
