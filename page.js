"use strict";
var RCNWorkbench;
(function (RCNWorkbench) {
    const searches = new Map();
    const datasets = new Map();
    let localMode = false;
    let selectedPath = "";
    let activeSearch = null;
    let rows = [];
    const styles = document.createElement("style");
    styles.textContent = `
    html{height:100%;overflow:hidden}
    body{height:100vh;height:100dvh;min-height:0;overflow:hidden}
    .top{position:sticky;top:0;flex:none}
    .layout{flex:1 1 auto;height:auto;min-height:0;overflow:hidden}
    .map-wrap{height:100%;min-height:0}
    .bottom{flex:0 0 180px;height:180px;min-height:0}
    body:not([data-view]) .workbench{display:none}
    .workbench{flex:1 1 auto;min-height:0;overflow:hidden;padding:20px;background:#f3f6f3;border-top:1px solid #dce5dd;color:#15251f}
    .workbench-inner{display:flex;flex-direction:column;height:100%;min-height:0;max-width:1450px;margin:auto;background:#fff;border:1px solid #dce5dd;border-radius:14px;padding:18px}
    .workbench h2{margin:0 0 5px;font-size:18px}.workbench p{margin:4px 0;color:#718077;font-size:12px;line-height:1.55}
    .workbench-toolbar,.workbench-presets,.saved-searches,.local-import,.new-search{display:flex;flex-wrap:wrap;gap:8px;align-items:center;margin-top:12px}
    .workbench button,.workbench select,.workbench input,.workbench textarea{font:inherit;color:#30443a;background:#fff;border:1px solid #d9e3dc;border-radius:8px;padding:8px 10px}
    .workbench button{cursor:pointer;font-size:12px;font-weight:650}.workbench button:hover{background:#f3f8f4}
    .workbench input[type=search]{flex:1;min-width:220px}.workbench input[type=file]{max-width:100%;font-size:11px}
    .region-button{display:grid;text-align:left;gap:4px;min-width:225px}.region-button small{font-weight:400;color:#718077}
    .workbench-status{min-height:20px;margin-top:8px!important}.workbench-results{flex:1;min-height:0;overflow:auto;margin-top:12px;border:1px solid #e6ece7;border-radius:8px}
    .workbench table{width:100%;border-collapse:collapse;font-size:11px;white-space:nowrap}.workbench th,.workbench td{padding:7px 9px;border-bottom:1px solid #edf1ed;text-align:left}
    .workbench th{position:sticky;top:0;background:#f7faf7;color:#52645a}.workbench-empty{padding:18px!important;color:#78867d}
    .workbench .local-panel{margin-top:16px;padding:14px;border:1px solid #dce5dd;border-radius:10px;background:#fafcf9}
    .workbench .local-panel>summary{font-size:14px;font-weight:750;cursor:pointer}
    .workbench .local-panel>summary::marker{color:#397554}
    .workbench .local-tools{padding-top:10px}
    .workbench .local-tabs{display:flex;gap:6px;flex-wrap:wrap;border-bottom:1px solid #dce5dd;margin-bottom:12px}
    .workbench .local-tab{border:0;border-bottom:2px solid transparent;border-radius:7px 7px 0 0;padding:9px 12px;color:#627268;background:transparent}
    .workbench .local-tab[aria-selected=true]{color:#17643e;border-bottom-color:#26734d;background:#edf7f0}
    .workbench .local-tab-panel{padding:2px 0}
    .workbench .local-tab-panel[hidden]{display:none}
    .workbench .form-field{display:grid;gap:4px;min-width:170px;flex:1;font-size:11px;color:#627268}
    .workbench .form-field.wide{min-width:280px}.workbench .local-panel[hidden]{display:none}
    .workbench .rcn-online{margin:12px 0;padding:12px;border:1px solid #dce5dd;border-radius:9px;background:#fff}
    .workbench .rcn-online-head{display:flex;align-items:center;justify-content:space-between;gap:8px}
    .workbench .rcn-online-list{display:grid;grid-template-columns:repeat(auto-fit,minmax(240px,1fr));gap:7px;margin-top:8px}
    .workbench .rcn-online-item{display:flex;align-items:center;justify-content:space-between;gap:8px;padding:8px;border:1px solid #e6ece7;border-radius:8px}
    .workbench .rcn-online-item strong{display:block;font-size:11px}
    .workbench .rcn-online-item small{display:block;margin-top:3px;color:#718077;font-size:10px}
    .workbench .rcn-online-item button{flex:none;padding:6px 8px;font-size:10px}
    .workbench .rcn-online-status{font-size:10px;color:#397554}
    .workbench .rcn-online-status.outdated{color:#a05d15}
    .workbench .danger{color:#8e382c}.workbench .mode-note{font-size:11px}
    @media(max-width:760px){
      .layout{display:flex;flex-direction:column}
      .map-wrap{flex:1 1 auto;height:auto;min-height:0}
      .side{flex:0 0 31%;height:auto;min-height:0}
      .bottom{flex:0 0 150px;height:150px;grid-template-columns:1fr}
      .workbench{padding:10px}
      .workbench-inner{padding:12px;max-width:none}
      .workbench-toolbar>*{width:100%}
      .region-button{width:100%}
      .workbench .local-tab{flex:1;padding:8px 6px;font-size:11px}
    }
  `;
    document.head.append(styles);
    const header = document.querySelector(".top");
    const brand = header.querySelector(".brand");
    const brandIcon = brand.querySelector(".brandmark");
    brandIcon.innerHTML = `<svg viewBox="0 0 48 48" aria-hidden="true"><path d="m7 12 11-4 12 4 11-4v28l-11 4-12-4-11 4V12Z" fill="none" stroke="currentColor" stroke-width="3" stroke-linejoin="round"/><path d="M18 8v28M30 12v28" fill="none" stroke="currentColor" stroke-width="3"/><path d="m11 18 4-2m-4 8 4-2m17-4 5-2m-5 8 5-2" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"/></svg>`;
    brandIcon.setAttribute("aria-label", "Mapa działek");
    const navigation = document.createElement("nav");
    navigation.className = "top-nav";
    navigation.setAttribute("aria-label", "Główna nawigacja");
    navigation.innerHTML = `
    <button class="top-nav-tab" type="button" data-view="map" aria-current="page">Mapa</button>
    <button class="top-nav-tab" type="button" data-view="search">Wyszukiwarka</button>
    <button class="top-nav-tab" type="button" data-view="import">Import</button>
  `;
    brand.after(navigation);
    const section = document.createElement("section");
    section.className = "workbench";
    section.innerHTML = `
    <div class="workbench-inner">
      <p class="import-notice" id="importNotice" hidden>Import RCN jest dostępny w lokalnej wersji aplikacji. Uruchom ją przez Docker Compose, aby importować pliki.</p>
      <h2>Wyszukiwania RCN</h2>
      <p id="pageMode"></p>
      <div class="workbench-presets" id="searches"></div>
      <div class="workbench-toolbar">
        <label>Otwórz CSV <input id="csvFiles" type="file" accept=".csv,text/csv" multiple></label>
        <select id="datasetSelect" aria-label="Wybrany plik CSV"></select>
        <input id="rowFilter" type="search" placeholder="Filtruj rekordy po dowolnym polu…" aria-label="Filtruj rekordy">
        <button id="saveFilter" type="button" hidden>Zapisz filtr jako wyszukiwanie</button>
      </div>
      <p id="activeDescription"></p>
      <p class="workbench-status" id="workbenchStatus" role="status" aria-live="polite"></p>
      <p id="resultCount"></p>
      <div class="workbench-results" id="workbenchResults"></div>
      <details class="local-panel" id="localPanel" hidden>
        <summary>Lokalny import, archiwa i nowe wyszukiwania</summary>
        <div class="local-tools">
        <p>Pliki GML trafią do Data/RCN/, a wygenerowane wyniki do Data/CSV/.</p>
        <div class="local-tabs" role="tablist" aria-label="Lokalne narzędzia RCN">
          <button class="local-tab" id="tabArchives" type="button" role="tab" aria-selected="true" aria-controls="panelArchives">Archiwa GEOPOZ</button>
          <button class="local-tab" id="tabImport" type="button" role="tab" aria-selected="false" aria-controls="panelImport">Import GML</button>
          <button class="local-tab" id="tabCreate" type="button" role="tab" aria-selected="false" aria-controls="panelCreate">Nowe wyszukiwanie</button>
        </div>
        <div class="local-tab-panel" id="panelArchives" role="tabpanel" aria-labelledby="tabArchives">
        <div class="rcn-online">
          <div class="rcn-online-head">
            <strong>Archiwa GEOPOZ</strong>
            <button id="refreshRCNArchives" type="button">Odśwież listę</button>
          </div>
          <p>Nową wersję wykrywamy po zmianie daty lub rozmiaru podanych przez GEOPOZ. Przy pobraniu zapisujemy też SHA-256 i nagłówki serwera, jeśli są dostępne.</p>
          <p id="rcnArchiveStatus" role="status" aria-live="polite">Ładowanie listy plików…</p>
          <div class="rcn-online-list" id="rcnArchiveList"></div>
        </div>
        </div>
        <div class="local-tab-panel" id="panelImport" role="tabpanel" aria-labelledby="tabImport" hidden>
        <div class="local-import">
          <label>Importuj pliki RCN (.gml) <input id="gmlFiles" type="file" accept=".gml,application/gml+xml,application/xml,text/xml" multiple></label>
          <button id="uploadGml" type="button">Importuj do Data/RCN/</button>
          <select id="rcnFiles" multiple size="3" aria-label="Wybierz wejściowe pliki RCN"></select>
        </div>
        </div>
        <div class="local-tab-panel" id="panelCreate" role="tabpanel" aria-labelledby="tabCreate" hidden>
        <div class="new-search">
          <label class="form-field">Nazwa wyszukiwania<input id="searchName" value="Umultowo"></label>
          <label class="form-field">Rodzaj filtra
            <select id="searchMode"><option value="radius">Promień</option><option value="streets">Ulice</option></select>
          </label>
          <label class="form-field">Działka punktu (opcjonalnie)<input id="parcel" value="306401_1.0050.AR_02.2/38"></label>
          <label class="form-field">Punkt WGS84: szer., dł.<input id="center" value="52.4263,16.9351"></label>
          <label class="form-field">Promień (m)<input id="radius" type="number" min="1" value="1050"></label>
          <label class="form-field wide">Ulice, po przecinku<input id="streets" value="Pszczelna,Zagonowa,Winna,Warzywna,Pasieka,Sadowa,Gronowa,Wyżyny,Winogrady"></label>
          <button id="createSearch" type="button">Wygeneruj CSV i dodaj wyszukiwanie</button>
        </div>
        <p class="mode-note">Tryb „promień” przyjmuje działkę albo punkt WGS84. Tryb „ulice” zapisuje spłaszczony CSV transakcji.</p>
        </div>
        </div>
      </details>
    </div>`;
    document.body.append(section);
    const localPanel = section.querySelector("#localPanel");
    const importNotice = section.querySelector("#importNotice");
    navigation.querySelectorAll(".top-nav-tab").forEach(tab => {
        tab.addEventListener("click", () => {
            const view = tab.dataset.view ?? "map";
            const importing = view === "import";
            if (importing) {
                document.body.dataset.view = "import";
                const isLocal = document.body.dataset.localMode === "true";
                localPanel.hidden = !isLocal;
                importNotice.hidden = isLocal;
                if (isLocal) {
                    localPanel.open = true;
                    section.querySelector("#tabImport").click();
                }
            }
            else if (view === "search") {
                document.body.dataset.view = "search";
                localPanel.hidden = true;
                localPanel.open = false;
                importNotice.hidden = true;
            }
            else {
                delete document.body.dataset.view;
                localPanel.hidden = document.body.dataset.localMode !== "true";
                localPanel.open = false;
                importNotice.hidden = true;
            }
            navigation.querySelectorAll(".top-nav-tab").forEach(candidate => {
                if (candidate === tab) {
                    candidate.setAttribute("aria-current", "page");
                }
                else {
                    candidate.removeAttribute("aria-current");
                }
            });
        });
    });
    const status = section.querySelector("#workbenchStatus");
    const datasetSelect = section.querySelector("#datasetSelect");
    const filterInput = section.querySelector("#rowFilter");
    const saveFilterButton = section.querySelector("#saveFilter");
    const toolTabs = [
        { button: section.querySelector("#tabArchives"), panel: section.querySelector("#panelArchives") },
        { button: section.querySelector("#tabImport"), panel: section.querySelector("#panelImport") },
        { button: section.querySelector("#tabCreate"), panel: section.querySelector("#panelCreate") }
    ];
    for (const [index, tab] of toolTabs.entries()) {
        tab.button.addEventListener("click", () => {
            for (const [candidateIndex, candidate] of toolTabs.entries()) {
                const selected = candidateIndex === index;
                candidate.button.setAttribute("aria-selected", String(selected));
                candidate.panel.hidden = !selected;
            }
        });
    }
    function report(message) {
        status.textContent = message;
    }
    async function responseError(response) {
        const text = await response.text();
        return new Error(text || `HTTP ${response.status}`);
    }
    function renderDatasets() {
        datasetSelect.replaceChildren();
        for (const [path, dataset] of datasets) {
            const option = document.createElement("option");
            option.value = path;
            option.textContent = dataset.label;
            option.selected = path === selectedPath;
            datasetSelect.append(option);
        }
        datasetSelect.disabled = datasets.size === 0;
    }
    function renderRows() {
        const query = filterInput.value.trim().toLocaleLowerCase("pl-PL");
        const matches = rows.filter(row => Object.values(row).some(value => String(value ?? "").toLocaleLowerCase("pl-PL").includes(query)));
        const headers = rows.length > 0 ? Object.keys(rows[0]) : [];
        section.querySelector("#resultCount").textContent =
            `Wyniki: ${matches.length} z ${rows.length}`;
        const container = section.querySelector("#workbenchResults");
        container.replaceChildren();
        if (matches.length === 0 || headers.length === 0) {
            const empty = document.createElement("p");
            empty.className = "workbench-empty";
            empty.textContent = "Brak rekordów do wyświetlenia.";
            container.append(empty);
            return;
        }
        const table = document.createElement("table");
        const head = table.createTHead().insertRow();
        for (const key of headers.slice(0, 10)) {
            const cell = document.createElement("th");
            cell.textContent = key;
            head.append(cell);
        }
        const body = table.createTBody();
        for (const row of matches.slice(0, 250)) {
            const tableRow = body.insertRow();
            for (const key of headers.slice(0, 10)) {
                tableRow.insertCell().textContent = String(row[key] ?? "");
            }
        }
        container.append(table);
        if (matches.length > 250) {
            const note = document.createElement("p");
            note.className = "workbench-empty";
            note.textContent = "Pokazano pierwsze 250 wyników; doprecyzuj filtr, aby zawęzić listę.";
            container.append(note);
        }
    }
    async function loadDataset(path, label) {
        const response = await fetch(path);
        if (!response.ok) {
            throw await responseError(response);
        }
        const datasetRows = parseCSV(await response.text());
        datasets.set(path, { path, label, rows: datasetRows });
        selectedPath = path;
        rows = datasetRows;
        renderDatasets();
        renderRows();
    }
    function searchPath(search) {
        return search.transactions ?? search.files[0];
    }
    async function openSearch(search) {
        activeSearch = search;
        filterInput.value = search.query ?? "";
        if (localMode) {
            section.querySelector("#searchName").value = search.name;
            section.querySelector("#searchMode").value =
                search.center ? "radius" : search.mode === "streets" ? "streets" : "radius";
            section.querySelector("#parcel").value = search.parcel ?? "";
            section.querySelector("#center").value = search.center ?? "";
            section.querySelector("#radius").value = String(search.radius ?? 500);
            section.querySelector("#streets").value = (search.streets ?? []).join(",");
        }
        section.querySelector("#activeDescription").textContent =
            `${search.name}: ${search.description}`;
        const path = searchPath(search);
        if (!path) {
            rows = [];
            renderRows();
            report(`Wpis „${search.name}” nie wskazuje pliku CSV.`);
            return;
        }
        try {
            await loadDataset(path, `${search.name} — ${path}`);
            renderRows();
            report(`Wczytano ${rows.length} rekordów z ${path}.`);
        }
        catch (error) {
            rows = [];
            renderRows();
            report(`Nie udało się wczytać ${path}: ${String(error)}`);
        }
        saveFilterButton.hidden = !localMode;
    }
    function renderSearches(items) {
        searches.clear();
        const container = section.querySelector("#searches");
        container.replaceChildren();
        for (const item of items) {
            searches.set(item.id, item);
            const button = document.createElement("button");
            button.type = "button";
            button.className = "region-button";
            const title = document.createElement("strong");
            title.textContent = item.name;
            const detail = document.createElement("small");
            detail.textContent = item.description || item.files.join(", ");
            button.append(title, detail);
            button.addEventListener("click", () => void openSearch(item));
            container.append(button);
        }
    }
    async function loadCatalog() {
        let items = [];
        try {
            const localResponse = await fetch("/api/searches");
            if (localResponse.ok && (localResponse.headers.get("content-type") ?? "").includes("application/json")) {
                items = await localResponse.json();
                localMode = true;
            }
        }
        catch {
            localMode = false;
        }
        if (!localMode) {
            try {
                const staticResponse = await fetch("Data/searches.json");
                if (!staticResponse.ok) {
                    throw await responseError(staticResponse);
                }
                items = await staticResponse.json();
            }
            catch (error) {
                report(`Nie udało się wczytać katalogu Data/searches.json: ${String(error)}`);
            }
        }
        section.querySelector("#pageMode").textContent = localMode
            ? "Tryb lokalny: można importować GML, generować CSV i zapisywać katalog wyszukiwań."
            : "Tryb GitHub Pages: tylko odczyt zatwierdzonych wyszukiwań i CSV. Nowe zestawy twórz lokalnie i commituj.";
        document.body.dataset.localMode = String(localMode);
        localPanel.hidden = !localMode;
        saveFilterButton.hidden = !localMode;
        renderSearches(items);
        const preferred = items.find(item => item.default) ??
            items.find(item => item.name.toLocaleLowerCase("pl-PL").includes("umultowo")) ?? items[0];
        if (preferred) {
            await openSearch(preferred);
        }
        else {
            report("Katalog nie zawiera jeszcze zapisanych wyszukiwań.");
        }
        if (localMode) {
            await refreshRCNFiles();
            await refreshRCNArchives().catch(error => {
                section.querySelector("#rcnArchiveStatus").textContent =
                    `Nie udało się pobrać listy archiwów GEOPOZ: ${String(error)}`;
            });
        }
    }
    async function refreshRCNFiles() {
        const response = await fetch("/api/rcn-files");
        if (!response.ok) {
            throw await responseError(response);
        }
        const files = await response.json();
        const select = section.querySelector("#rcnFiles");
        select.replaceChildren();
        for (const file of files) {
            const option = document.createElement("option");
            option.value = file.name;
            option.textContent = `${file.name} (${(file.size / (1024 * 1024)).toFixed(1)} MiB)`;
            select.append(option);
        }
        if (files.length === 0) {
            report("Folder Data/RCN/ jest pusty. Wybierz eksport GML powyżej albo skopiuj plik do Data/RCN/.");
        }
    }
    async function refreshRCNArchives() {
        const response = await fetch("/api/rcn-archives");
        if (!response.ok) {
            throw await responseError(response);
        }
        const result = await response.json();
        const status = section.querySelector("#rcnArchiveStatus");
        const list = section.querySelector("#rcnArchiveList");
        list.replaceChildren();
        status.textContent = `☁ ${result.archives.length} archiwów dostępnych online`;
        for (const archive of result.archives) {
            const item = document.createElement("div");
            item.className = "rcn-online-item";
            const details = document.createElement("div");
            const name = document.createElement("strong");
            name.textContent = archive.name;
            const info = document.createElement("small");
            info.textContent = archive.info || "Archiwum ZIP";
            const state = document.createElement("small");
            state.className = "rcn-online-status";
            if (archive.updateAvailable) {
                state.classList.add("outdated");
                state.textContent = "☁ Online · dostępna nowsza wersja";
            }
            else if (archive.downloaded && archive.freshnessKnown) {
                state.textContent = "☁ Online · ✓ pobrano do RCN/";
            }
            else if (archive.downloaded) {
                state.textContent = "☁ Online · ✓ w RCN/ · wersji nie można porównać";
            }
            else {
                state.textContent = "☁ Dostępne online · brak w RCN/";
            }
            details.append(name, info, state);
            item.append(details);
            const canDownload = !archive.downloaded || archive.updateAvailable ||
                (archive.managed && !archive.freshnessKnown);
            if (canDownload) {
                const button = document.createElement("button");
                button.type = "button";
                button.textContent = archive.updateAvailable ? "Zaktualizuj" : archive.downloaded ? "Ponów" : "Pobierz";
                button.addEventListener("click", () => void downloadRCNArchive(archive, button));
                item.append(button);
            }
            list.append(item);
        }
    }
    async function downloadRCNArchive(archive, button) {
        button.disabled = true;
        button.textContent = archive.downloaded ? "Aktualizuję…" : "Pobieram…";
        report(`${archive.downloaded ? "Aktualizowanie" : "Pobieranie"} archiwum „${archive.name}”…`);
        try {
            const response = await fetch(`/api/rcn-archives/${encodeURIComponent(archive.id)}/download`, {
                method: "POST"
            });
            if (!response.ok) {
                throw await responseError(response);
            }
            const result = await response.json();
            await refreshRCNFiles();
            await refreshRCNArchives();
            report(`Zapisano w RCN/: ${result.downloaded.join(", ")}.`);
        }
        catch (error) {
            button.disabled = false;
            button.textContent = archive.downloaded ? "Zaktualizuj" : "Pobierz";
            report(`Nie udało się pobrać „${archive.name}”: ${String(error)}`);
        }
    }
    async function uploadGML() {
        const input = section.querySelector("#gmlFiles");
        const files = Array.from(input.files ?? []);
        if (files.length === 0) {
            report("Wybierz co najmniej jeden plik GML.");
            return;
        }
        const form = new FormData();
        for (const file of files) {
            form.append("files", file, file.name);
        }
        const response = await fetch("/api/upload", { method: "POST", body: form });
        if (!response.ok) {
            throw await responseError(response);
        }
        const result = await response.json();
        input.value = "";
        await refreshRCNFiles();
        report(`Zaimportowano do Data/RCN/: ${result.uploaded.join(", ") || "brak nowych plików"}.`);
    }
    async function createSearch() {
        const fileSelect = section.querySelector("#rcnFiles");
        const files = Array.from(fileSelect.selectedOptions).map(option => option.value);
        if (files.length === 0) {
            report("Wybierz wejściowe pliki GML z listy Data/RCN/.");
            return;
        }
        const mode = section.querySelector("#searchMode").value;
        const center = section.querySelector("#center").value.trim();
        const payload = {
            name: section.querySelector("#searchName").value.trim(),
            mode,
            files,
            parcel: section.querySelector("#parcel").value.trim(),
            center,
            radius: Number(section.querySelector("#radius").value),
            streets: section.querySelector("#streets").value.split(",").map(value => value.trim()).filter(Boolean)
        };
        report("Przetwarzanie GML — może potrwać przy dużych plikach…");
        const response = await fetch("/api/searches", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(payload)
        });
        if (!response.ok) {
            throw await responseError(response);
        }
        const created = await response.json();
        searches.set(created.id, created);
        renderSearches(Array.from(searches.values()));
        await openSearch(created);
        report(`Utworzono CSV i zapisano wyszukiwanie „${created.name}”. Zatwierdź Data/CSV/ oraz Data/searches.json, aby opublikować je na GitHub Pages.`);
    }
    async function saveCurrentFilter() {
        if (!selectedPath.startsWith("Data/CSV/")) {
            report("Filtr można dopisać do katalogu tylko dla zatwierdzonego pliku CSV.");
            return;
        }
        const name = window.prompt("Nazwa wyszukiwania:", `${activeSearch?.name ?? "CSV"} — ${filterInput.value.trim() || "wszystkie rekordy"}`);
        if (!name?.trim()) {
            return;
        }
        const response = await fetch("/api/searches", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
                name: name.trim(),
                mode: "view",
                files: [selectedPath],
                query: filterInput.value.trim()
            })
        });
        if (!response.ok) {
            throw await responseError(response);
        }
        const saved = await response.json();
        searches.set(saved.id, saved);
        renderSearches(Array.from(searches.values()));
        report(`Zapisano filtr „${saved.name}” w Data/searches.json.`);
    }
    section.querySelector("#csvFiles").addEventListener("change", async (event) => {
        const files = Array.from(event.currentTarget.files ?? []);
        try {
            for (const file of files) {
                const path = `local:${file.name}`;
                const fileRows = parseCSV(await file.text());
                datasets.set(path, { path, label: file.name, rows: fileRows });
                selectedPath = path;
                rows = fileRows;
            }
            renderDatasets();
            renderRows();
            report(`Wczytano lokalnych plików CSV: ${files.length}.`);
            saveFilterButton.hidden = true;
        }
        catch (error) {
            report(`Nie udało się wczytać CSV: ${String(error)}`);
        }
    });
    datasetSelect.addEventListener("change", () => {
        selectedPath = datasetSelect.value;
        rows = datasets.get(selectedPath)?.rows ?? [];
        activeSearch = null;
        renderRows();
        saveFilterButton.hidden = !localMode || !selectedPath.startsWith("Data/CSV/");
    });
    filterInput.addEventListener("input", renderRows);
    saveFilterButton.addEventListener("click", () => void saveCurrentFilter().catch(error => report(`Nie udało się zapisać wyszukiwania: ${String(error)}`)));
    section.querySelector("#uploadGml").addEventListener("click", () => void uploadGML().catch(error => report(`Import GML nie powiódł się: ${String(error)}`)));
    section.querySelector("#refreshRCNArchives").addEventListener("click", () => void refreshRCNArchives().catch(error => section.querySelector("#rcnArchiveStatus").textContent =
        `Nie udało się pobrać listy archiwów GEOPOZ: ${String(error)}`));
    section.querySelector("#createSearch").addEventListener("click", () => void createSearch().catch(error => report(`Nie udało się utworzyć CSV: ${String(error)}`)));
    void loadCatalog().catch(error => report(`Nie udało się uruchomić przeglądarki danych: ${String(error)}`));
})(RCNWorkbench || (RCNWorkbench = {}));
