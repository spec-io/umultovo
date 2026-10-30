param(
    [string]$KatalogDanych = $PSScriptRoot,
    [string[]]$Ulice = @("Pszczelna","Zagonowa","Winna","Warzywna","Pasieka","Sadowa","Gronowa","Wyżyny","Winogrady"),
    [string[]]$Pliki = @("Baza Danych RCN Poznan 2021-2025.gml","Baza Danych RCN Poznan 2026.gml"),
    [string]$PrefiksWyjscia = "stare_winogrady"
)

function StripTs([string]$id) { $id -replace "_\d{4}-\d{2}-\d{2}T[\d\-]+$","" -replace "^#","" }

function MatchUlica([string]$u, [string[]]$lista) {
    foreach ($f in $lista) { if ($u -imatch [regex]::Escape($f)) { return $true } }
    return $false
}

$rodzajNier    = @{"1"="niezabudowana";"2"="zabudowana";"3"="budynkowa";"4"="lokalowa"}
$prawoNier     = @{"1"="wlasnosc";"2"="uzytkowanie_wieczyste";"3"="wlasnosc_lokalu_SM";"4"="inne"}
$rodzajTrans   = @{"1"="sprzedaz";"2"="zamiana";"3"="inna"}
$rodzajRynku   = @{"1"="pierwotny";"2"="wtorny"}
$stronaTrans   = @{"1"="os_fizyczna";"2"="os_prawna";"3"="mieszana"}
$sposobUzytk   = @{"1"="uzytek_rolny";"2"="grunt_lesny";"3"="grunt_zabudowany";"4"="tereny_rozne";"9"="inne"}
$funkcjaLokalu = @{"1"="mieszkalny";"2"="uzytkowy";"3"="garaza"}
$rodzajBud     = @{
    "100"="niemieszkalne";"101"="przemyslowe";"102"="magazynowe";"103"="handlowe";
    "104"="biurowe";"105"="transportu";"106"="kultura";"107"="oswiata";"108"="lecznictwo";
    "109"="goscinne";"110"="mieszkalne_jedno";"111"="mieszkalne_wielo";"112"="zbiorowe";
    "113"="rekreacji";"114"="inne_niemieszkalne"
}

function C([hashtable]$m, $k) { if ($k -and $m.ContainsKey($k)) { $m[$k] } else { $k } }

function ParsujPlik([string]$path, [string]$nazwaPliku) {
    Write-Host "Przetwarzam: $nazwaPliku ..."
    $dokumenty  = @{}
    $budynki    = @{}
    $lokale     = @{}
    $dzialki    = @{}
    $nieruch    = @{}
    $transakcje = @{}

    $typ = $null; $id = $null; $d = $null; $n = 0

    $rdr = [System.IO.StreamReader]::new($path, [System.Text.Encoding]::UTF8)
    try {
        while (-not $rdr.EndOfStream) {
            $ln = $rdr.ReadLine().Trim(); $n++
            if ($n % 1000000 -eq 0) { Write-Host "  $([math]::Round($n/1000))k" }

            if ($ln -match '^<rcn:(RCN_Dokument|RCN_Budynek|RCN_Lokal|RCN_Dzialka|RCN_Nieruchomosc|RCN_Transakcja)\s') {
                $typ = $Matches[1]
                $id  = if ($ln -match 'gml:id="([^"]+)"') { $Matches[1] } else { "" }
                $d   = @{ gml_id=$id; plik=$nazwaPliku; refs=@{bud=@();dz=@();lok=@();dok=$null;nier=$null} }
                continue
            }
            if (-not $typ) { continue }

            # --- pola tekstowe ---
            $fmap = @{
                "oznaczenieDokumentu"                   = "oznaczenie_dok"
                "dataSporzadzeniaDokumentu"              = "data_aktu"
                "tworcaDokumentu"                        = "notariusz"
                "idBudynku"                              = "id_budynku"
                "rodzajBudynku"                          = "rodzaj_budynku_kod"
                "powierzchniaUzytkowaBudynku"            = "pow_uzytkowa_budynku_m2"
                "idDzialki"                              = "id_dzialki"
                "przeznaczenieWMPZP"                     = "przeznaczenie_mpzp"
                "polePowierzchniEwidencyjnej"            = "pow_ewidencyjna_ha"
                "sposobUzytkowania"                      = "sposob_uzytkowania_kod"
                "cenaDzialkiEwidencyjnejBrutto"         = "cena_dzialki_brutto"
                "powUzytkowaLokalu"                      = "pow_lokalu_m2"
                "cenaLokaluBrutto"                       = "cena_lokalu_brutto"
                "funkcjaLokalu"                          = "funkcja_lokalu_kod"
                "liczbaIzb"                              = "liczba_izb"
                "nrKondygnacji"                          = "kondygnacja"
                "rodzajNieruchomosci"                    = "rodzaj_nieruchomosci_kod"
                "rodzajPrawaDoNieruchomosci"             = "prawo_do_nier_kod"
                "udzialWPrawieDoNieruchomosci"           = "udzial_w_prawie"
                "polePowierzchniNieruchomosciGruntowej"  = "pow_gruntowa_ha"
                "cenaNieruchomosciBrutto"                = "cena_nieruchomosci_brutto"
                "oznaczenieTransakcji"                   = "numer_transakcji"
                "rodzajTransakcji"                       = "rodzaj_transakcji_kod"
                "rodzajRynku"                            = "rodzaj_rynku_kod"
                "stronaSprzedajaca"                      = "strona_sprzedajaca_kod"
                "stronaKupujaca"                         = "strona_kupujaca_kod"
                "cenaTransakcjiBrutto"                   = "cena_transakcji_brutto"
                "ulica"                                  = "ulica"
                "numerPorzadkowy"                        = "numer"
                "miejscowosc"                            = "miejscowosc"
            }
            foreach ($pole in $fmap.Keys) {
                if ($ln -match "^<rcn:$pole[^>]*>([^<]+)</rcn:$pole>") {
                    $d[$fmap[$pole]] = $Matches[1]; break
                }
            }

            # --- referencje ---
            if ($ln -match 'xlink:href="([^"]+)"') {
                $ref = StripTs $Matches[1]
                if      ($ln -match '^<rcn:budynek')       { $d.refs.bud  += $ref }
                elseif  ($ln -match '^<rcn:dzialka')        { $d.refs.dz   += $ref }
                elseif  ($ln -match '^<rcn:lokal')          { $d.refs.lok  += $ref }
                elseif  ($ln -match '^<rcn:podstawaPrawna') { $d.refs.dok   = $ref }
                elseif  ($ln -match '^<rcn:nieruchomosc')   { $d.refs.nier  = $ref }
            }

            # --- zamkniecie ---
            switch ($ln) {
                "</rcn:RCN_Dokument>"     { $dokumenty[(StripTs $id)]  = $d; $typ=$null }
                "</rcn:RCN_Budynek>"      { $budynki[(StripTs $id)]    = $d; $typ=$null }
                "</rcn:RCN_Lokal>"        { $lokale[(StripTs $id)]     = $d; $typ=$null }
                "</rcn:RCN_Dzialka>"      { $dzialki[(StripTs $id)]    = $d; $typ=$null }
                "</rcn:RCN_Nieruchomosc>" { $nieruch[(StripTs $id)]    = $d; $typ=$null }
                "</rcn:RCN_Transakcja>"   { $transakcje[(StripTs $id)] = $d; $typ=$null }
            }
        }
    } finally { $rdr.Dispose() }

    Write-Host "  Dok:$($dokumenty.Count) Bud:$($budynki.Count) Lok:$($lokale.Count) Dz:$($dzialki.Count) Nier:$($nieruch.Count) Trans:$($transakcje.Count)"

    # ---------- LACZENIE ----------
    $wyniki = [System.Collections.Generic.List[object]]::new()

    foreach ($tid in $transakcje.Keys) {
        $t = $transakcje[$tid]

        # Nieruchomosc
        $nid = $t.refs.nier
        $n   = if ($nid -and $nieruch[$nid]) { $nieruch[$nid] } else { @{refs=@{bud=@();dz=@();lok=@()}} }

        # Dokument (data)
        $dok = $null
        if ($t.refs.dok -and $dokumenty[$t.refs.dok]) { $dok = $dokumenty[$t.refs.dok] }

        # Budynki
        $budList = @()
        foreach ($bid in $n.refs.bud) {
            if ($budynki[$bid]) { $budList += $budynki[$bid] }
        }

        # Dzialki
        $dzList = @()
        foreach ($did in $n.refs.dz) {
            if ($dzialki[$did]) { $dzList += $dzialki[$did] }
        }
        # Ulice do filtrowania

        # Ustal ulice do sprawdzenia
        $adresy = @()
        foreach ($b in $budList) { if ($b["ulica"]) { $adresy += $b["ulica"] } }
        foreach ($l in $lokList) { if ($l["ulica"]) { $adresy += $l["ulica"] } }
        foreach ($dz in $dzList) { if ($dz["ulica"]) { $adresy += $dz["ulica"] } }

        $pasuje = $false
        foreach ($u in $adresy) { if (MatchUlica $u $ULICE) { $pasuje = $true; break } }
        if (-not $pasuje) { continue }

        # Buduj rekord
        $rec = [ordered]@{}

        # Transakcja
        $rec["data_aktu"]              = if ($dok) { $dok["data_aktu"] }          else { $null }
        $rec["notariusz"]              = if ($dok) { $dok["notariusz"] }           else { $null }
        $rec["oznaczenie_dokumentu"]   = if ($dok) { $dok["oznaczenie_dok"] }     else { $null }
        $rec["numer_transakcji"]       = $t["numer_transakcji"]
        $rec["rodzaj_transakcji"]      = C $rodzajTrans  $t["rodzaj_transakcji_kod"]
        $rec["rodzaj_rynku"]           = C $rodzajRynku  $t["rodzaj_rynku_kod"]
        $rec["strona_sprzedajaca"]     = C $stronaTrans  $t["strona_sprzedajaca_kod"]
        $rec["strona_kupujaca"]        = C $stronaTrans  $t["strona_kupujaca_kod"]
        $rec["cena_transakcji_brutto"] = if ($t["cena_transakcji_brutto"]) { [double]$t["cena_transakcji_brutto"] } else { $null }

        # Nieruchomosc
        $rec["rodzaj_nieruchomosci"]   = C $rodzajNier  $n["rodzaj_nieruchomosci_kod"]
        $rec["prawo_do_nieruchomosci"] = C $prawoNier   $n["prawo_do_nier_kod"]
        $rec["udzial_w_prawie"]        = $n["udzial_w_prawie"]
        $rec["pow_gruntowa_ha"]        = if ($n["pow_gruntowa_ha"])  { [double]$n["pow_gruntowa_ha"] }  else { $null }
        $rec["cena_nieruchomosci_brutto"] = if ($n["cena_nieruchomosci_brutto"]) { [double]$n["cena_nieruchomosci_brutto"] } else { $null }

        # Budynki
        $budArr = @()
        foreach ($b in $budList) {
            $budArr += [ordered]@{
                id_budynku        = $b["id_budynku"]
                ulica             = $b["ulica"]
                numer             = $b["numer"]
                rodzaj_budynku    = C $rodzajBud $b["rodzaj_budynku_kod"]
                pow_uzytkowa_m2   = if ($b["pow_uzytkowa_budynku_m2"]) { [double]$b["pow_uzytkowa_budynku_m2"] } else { $null }
            }
        }
        $rec["budynki"] = $budArr

        # Dzialki
        $dzArr = @()
        foreach ($dz in $dzList) {
            $dzArr += [ordered]@{
                id_dzialki          = $dz["id_dzialki"]
                ulica               = $dz["ulica"]
                numer               = $dz["numer"]
                przeznaczenie_mpzp  = $dz["przeznaczenie_mpzp"]
                pow_ewidencyjna_ha  = if ($dz["pow_ewidencyjna_ha"]) { [double]$dz["pow_ewidencyjna_ha"] } else { $null }
                sposob_uzytkowania  = C $sposobUzytk $dz["sposob_uzytkowania_kod"]
                cena_dzialki_brutto = if ($dz["cena_dzialki_brutto"]) { [double]$dz["cena_dzialki_brutto"] } else { $null }
            }
        }
        $rec["dzialki"] = $dzArr

        # Lokale
        $lokArr = @()
        foreach ($l in $lokList) {
            $lokArr += [ordered]@{
                ulica          = $l["ulica"]
                numer          = $l["numer"]
                funkcja        = C $funkcjaLokalu $l["funkcja_lokalu_kod"]
                pow_lokalu_m2  = if ($l["pow_lokalu_m2"]) { [double]$l["pow_lokalu_m2"] } else { $null }
                liczba_izb     = $l["liczba_izb"]
                kondygnacja    = $l["kondygnacja"]
                cena_lokalu_brutto = if ($l["cena_lokalu_brutto"]) { [double]$l["cena_lokalu_brutto"] } else { $null }
            }
        }
        $rec["lokale"] = $lokArr

        $rec["plik_zrodlowy"] = $nazwaPliku
        $wyniki.Add($rec)
    }

    Write-Host "  Pasujace transakcje: $($wyniki.Count)"
    return $wyniki
}

# ---- GLOWNA PETLA ----
$katalog = if ($KatalogDanych) { $KatalogDanych } else { Split-Path $MyInvocation.MyCommand.Path }

$wszystko = [System.Collections.Generic.List[object]]::new()
$rzeczywistePliki = @{
    "Baza Danych RCN Poznan 2021-2025.gml" = "Baza Danych RCN Pozna$([char]324) 2021-2025.gml"
    "Baza Danych RCN Poznan 2025.gml"      = "Baza Danych RCN Pozna$([char]324) 2025.gml"
    "Baza Danych RCN Poznan 2026.gml"      = "Baza Danych RCN Pozna$([char]324) 2026.gml"
    "Baza Danych RCN Poznan 2026-old.gml"  = "Baza Danych RCN Pozna$([char]324) 2026-old.gml"
}

foreach ($kluczPliku in $PLIKI) {
    $rzeczywistaNazwa = $rzeczywistePliki[$kluczPliku]
    $sciezka = Join-Path $katalog $rzeczywistaNazwa
    if (-not (Test-Path $sciezka)) {
        Write-Host "[POMINIETY] $sciezka"
        continue
    }
    $wyniki = ParsujPlik -path $sciezka -nazwaPliku $rzeczywistaNazwa
    foreach ($w in $wyniki) { $wszystko.Add($w) }
}

# ---- ZAPIS ----
$outJson = Join-Path $katalog "$PrefiksWyjscia.json"
$outCsv  = Join-Path $katalog "$PrefiksWyjscia.csv"

# JSON
$wszystko | ConvertTo-Json -Depth 6 |
    ForEach-Object { [System.Text.Encoding]::UTF8.GetBytes($_) } |
    Set-Content -Path $outJson -Encoding Byte

# CSV (flat - budynki/dzialki rozwijamy)
$csvRows = [System.Collections.Generic.List[object]]::new()
foreach ($r in $wszystko) {
    $base = [ordered]@{
        data_aktu              = $r["data_aktu"]
        notariusz              = $r["notariusz"]
        numer_transakcji       = $r["numer_transakcji"]
        rodzaj_transakcji      = $r["rodzaj_transakcji"]
        rodzaj_rynku           = $r["rodzaj_rynku"]
        cena_transakcji_brutto = $r["cena_transakcji_brutto"]
        rodzaj_nieruchomosci   = $r["rodzaj_nieruchomosci"]
        prawo_do_nieruchomosci = $r["prawo_do_nieruchomosci"]
        udzial_w_prawie        = $r["udzial_w_prawie"]
        pow_gruntowa_ha        = $r["pow_gruntowa_ha"]
    }
    # Pierwszy budynek
    if ($r["budynki"] -and $r["budynki"].Count -gt 0) {
        $b = $r["budynki"][0]
        $base["ulica"]           = $b["ulica"]
        $base["numer"]           = $b["numer"]
        $base["rodzaj_budynku"]  = $b["rodzaj_budynku"]
        $base["pow_budynku_m2"]  = $b["pow_uzytkowa_m2"]
    } elseif ($r["lokale"] -and $r["lokale"].Count -gt 0) {
        $base["ulica"] = $r["lokale"][0]["ulica"]; $base["numer"] = $r["lokale"][0]["numer"]
        $base["rodzaj_budynku"]=$null; $base["pow_budynku_m2"]=$null
    } else { $base["ulica"]=$null;$base["numer"]=$null;$base["rodzaj_budynku"]=$null;$base["pow_budynku_m2"]=$null }
    # Pierwsza dzialka
    if ($r["dzialki"] -and $r["dzialki"].Count -gt 0) {
        $dz = $r["dzialki"][0]
        $base["id_dzialki"]         = $dz["id_dzialki"]
        $base["przeznaczenie_mpzp"] = $dz["przeznaczenie_mpzp"]
        $base["pow_ewidencyjna_ha"] = $dz["pow_ewidencyjna_ha"]
    } else { $base["id_dzialki"]=$null;$base["przeznaczenie_mpzp"]=$null;$base["pow_ewidencyjna_ha"]=$null }
    # Pierwszy lokal
    if ($r["lokale"] -and $r["lokale"].Count -gt 0) {
        $l = $r["lokale"][0]
        $base["pow_lokalu_m2"]   = $l["pow_lokalu_m2"]
        $base["liczba_izb"]      = $l["liczba_izb"]
        $base["kondygnacja"]     = $l["kondygnacja"]
        $base["funkcja_lokalu"]  = $l["funkcja"]
    } else { $base["pow_lokalu_m2"]=$null;$base["liczba_izb"]=$null;$base["kondygnacja"]=$null;$base["funkcja_lokalu"]=$null }
    $base["plik"] = $r["plik_zrodlowy"]
    $csvRows.Add([PSCustomObject]$base)
}

$csvRows | Export-Csv -Path $outCsv -Delimiter ";" -Encoding UTF8 -NoTypeInformation

Write-Host ""
Write-Host "Zapisano $($wszystko.Count) transakcji"
Write-Host "JSON: $outJson"
Write-Host "CSV : $outCsv"
Write-Host ""
Write-Host "Podglad:"
$csvRows | Select-Object -First 10 | Format-Table data_aktu, ulica, numer, id_dzialki, cena_transakcji_brutto -AutoSize
