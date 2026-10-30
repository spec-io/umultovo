# Umultovo — analiza danych RCN

Projekt zawiera statyczną stronę mapy oraz lokalne narzędzie do przetwarzania eksportów
RCN (GML). GitHub Pages publikuje wyłącznie statyczne pliki: stronę, katalog wyszukiwań
i zatwierdzone zestawy CSV. Import GML, filtrowanie i zapis nowych wyników wykonuje
lokalny program Go.

## Układ projektu

- `Data/RCN/` — wejściowe pliki GML. Domyślnie ignorowane przez Git, żeby przypadkowo nie
  publikować dużych eksportów na GitHub Pages.
- `Source/` — narzędzie Go: parsowanie RCN, filtry promienia/ulic i lokalny serwer.
- `Data/CSV/` — aktywne, wygenerowane pliki CSV czytane przez stronę.
- `Data/searches.json` — katalog wyszukiwań publikowany razem z CSV.
- `Data/archive/` — historyczne raporty, JSON i poprzednie wersje mapy.
- `web/app.ts` — TypeScript interfejsu przeglądania; `page.js` to jego wersja dla
  przeglądarki.
- `index.html` — mapa i przeglądarka danych używane także przez GitHub Pages.

## GitHub Pages

GitHub Pages nie uruchamia Go ani nie zapisuje plików w repozytorium. Strona działa
więc w trybie tylko do odczytu: domyślnie wybiera Umultowo i wczytuje zatwierdzony
katalog z `Data/searches.json` oraz wskazane w nim CSV. Po utworzeniu nowych danych
lokalnie dodaj do commita wygenerowane pliki z `Data/CSV/` i zmiany w katalogu
wyszukiwań.

## Praca lokalna w kontenerze

Wymagany jest tylko Docker z Docker Compose. Go, Node.js i TypeScript są używane
wewnątrz obrazu — nie trzeba instalować ich na hoście.

```sh
LOCAL_UID=$(id -u) LOCAL_GID=$(id -g) docker compose up --build
```

Strona jest dostępna pod `http://umultovo.local:8081`. Port `8080` pozostaje
przypisany do ioHome, więc oba serwisy działają niezależnie. Jeśli komputer nie rozwiązuje
lokalnie nazw `.local` przez mDNS, dodaj jednorazowo wpis do pliku hosts:

```text
127.0.0.1 umultovo.local
```

Na Linuksie plikiem jest `/etc/hosts`; na Windows `C:\Windows\System32\drivers\etc\hosts`.
Port jest publikowany wyłącznie na interfejsie lokalnym.

Strona pozwala zaimportować pliki GML do `Data/RCN/`, wybrać pliki wejściowe, utworzyć
wyszukiwanie promieniem albo ulicami oraz przeglądać i filtrować zapisane CSV. Nowe
wyszukiwania dopisują się do `Data/searches.json`. Pliki GML nie są nadpisywane podczas
ręcznego importu; jeśli nazwa już istnieje, wybierz inną. Pobranie nowszego archiwum
może zastąpić pliki wcześniej pobrane przez ten panel.

Panel „Archiwa GEOPOZ” pobiera aktualną listę ZIP-ów z BIP, pokazuje które archiwa
mają swoje pliki GML w lokalnym `RCN/` i pozwala pobrać lub zaktualizować archiwum.
Pliki są wypakowywane do `Data/RCN/`, a ich stan i metadane pobrania zapisują się w
`Data/rcn-archives.json`. GEOPOZ nie publikuje na tej stronie sum kontrolnych; wykrywanie
nowszej wersji opiera się na publikowanym rozmiarze i dacie, a przy pobraniu lokalnie
zapisujemy SHA-256 oraz nagłówki `ETag` i `Last-Modified`, jeśli serwer je udostępnia.

Zatrzymanie kontenera:

```sh
docker compose down
```

Zapisane filtry tekstowe można dodać do katalogu dla już istniejącego CSV. Po
wygenerowaniu danych sprawdź zmiany, a następnie zatwierdź `Data/CSV/` i
`Data/searches.json`, aby pojawiły się na GitHub Pages.

## Polecenia Go bez interfejsu

Można je uruchomić także wewnątrz kontenera, np. `docker compose run --rm app radius
--help`. Bez listy plików program czyta `Data/RCN/*.gml`.

```sh
# Umultowo: działka 2/38 i promień 500 m
docker compose run --rm app radius --parcel 306401_1.0050.AR_02.2/38 --radius 500 \
  --output ../CSV/okolica_2-38_500m

# Winogrady: punkt WGS84 i promień 1 050 m
docker compose run --rm app radius --center 52.4263,16.9351 --radius 1050 \
  --output ../CSV/winogrady-1050m

# Historyczny filtr ulic
docker compose run --rm app streets --streets Pszczelna,Zagonowa,Winna,Warzywna,Pasieka,Sadowa,Gronowa,Wyżyny,Winogrady \
  --output ../CSV/stare_winogrady
```

Polecenia Go bezpośrednio zapisują pliki CSV, a interfejs lokalny tworzy także wpis
w katalogu wyszukiwań. Filtr promienia zapisuje `-obiekty.csv` i `-transakcje.csv`.
Filtr ulic zapisuje spłaszczony CSV i JSON. Środek
Winogradów (`52.4263,16.9351`) i promień `1 050 m` dobrano na podstawie zasięgu
ulic z archiwalnego zestawu w OpenStreetMap; obejmuje ich wspólny obrys z około
100-metrowym marginesem i jest wartością startową do weryfikacji na aktualnym GML.

## Weryfikacja

```sh
cd Source
go test ./...
cd ..
npm run build
```

Oba polecenia weryfikacyjne są dostępne bez instalacji na hoście przez:

```sh
docker compose build
```