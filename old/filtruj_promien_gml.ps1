param(
    [string]$KatalogDanych = $PSScriptRoot,
    [string]$DzialkaPunktu = "306401_1.0050.AR_02.2/38",
    [double]$PromienMetry = 500,
    [string[]]$Pliki = @("Baza Danych RCN Poznan 2025.gml", "Baza Danych RCN Poznan 2026.gml"),
    [string]$PrefiksWyjscia = "okolica_2-38_500m"
)

Add-Type -AssemblyName System.Xml.Linq
$culture = [System.Globalization.CultureInfo]::InvariantCulture

function Strip-Timestamp([string]$Id) {
    if (-not $Id) { return "" }
    return (($Id -replace '^#', '') -replace '_\d{4}-\d{2}-\d{2}T[\d\-:]+$', '')
}

function Get-DescendantText($Element, [string]$LocalName) {
    $node = $Element.Descendants() | Where-Object { $_.Name.LocalName -eq $LocalName } | Select-Object -First 1
    if ($node) { return [string]$node.Value }
    return $null
}

function Get-PolygonPoints($Element) {
    $posList = $Element.Descendants() | Where-Object { $_.Name.LocalName -eq 'posList' } | Select-Object -First 1
    if (-not $posList) { return ,@() }
    $values = @($posList.Value -split '\s+' | Where-Object { $_ } | ForEach-Object { [double]::Parse($_, $culture) })
    $points = [System.Collections.Generic.List[object]]::new()
    for ($i = 0; $i + 1 -lt $values.Count; $i += 2) {
        # RCN uses EPSG:2177 coordinates in northing,easting order in posList.
        $points.Add([pscustomobject]@{ X = $values[$i + 1]; Y = $values[$i] })
    }
    return ,$points.ToArray()
}

function Get-PolygonCentroid($Points) {
    $area2 = 0.0; $cx = 0.0; $cy = 0.0
    for ($i = 0; $i -lt $Points.Count - 1; $i++) {
        $cross = $Points[$i].X * $Points[$i + 1].Y - $Points[$i + 1].X * $Points[$i].Y
        $area2 += $cross
        $cx += ($Points[$i].X + $Points[$i + 1].X) * $cross
        $cy += ($Points[$i].Y + $Points[$i + 1].Y) * $cross
    }
    if ([math]::Abs($area2) -lt 0.001) {
        $unique = @($Points | Select-Object -First ($Points.Count - 1))
        return [pscustomobject]@{ X = ($unique | Measure-Object X -Average).Average; Y = ($unique | Measure-Object Y -Average).Average }
    }
    return [pscustomobject]@{ X = $cx / (3 * $area2); Y = $cy / (3 * $area2) }
}

function Test-PointInPolygon($Point, $Points) {
    $inside = $false
        $j = $Points.Count - 1
        for ($i = 0; $i -lt $Points.Count; $i++) {
        $a = $Points[$i]; $b = $Points[$j]
        if ((($a.Y -gt $Point.Y) -ne ($b.Y -gt $Point.Y)) -and
            ($Point.X -lt (($b.X - $a.X) * ($Point.Y - $a.Y) / (($b.Y - $a.Y) + 1e-20) + $a.X))) {
            $inside = -not $inside
        }
            $j = $i
    }
    return $inside
}

function Get-DistanceToSegment($Point, $A, $B) {
    $dx = $B.X - $A.X; $dy = $B.Y - $A.Y
    $length2 = $dx * $dx + $dy * $dy
    if ($length2 -le 0) { return [math]::Sqrt([math]::Pow($Point.X - $A.X, 2) + [math]::Pow($Point.Y - $A.Y, 2)) }
    $t = (($Point.X - $A.X) * $dx + ($Point.Y - $A.Y) * $dy) / $length2
    $t = [math]::Max(0, [math]::Min(1, $t))
    $x = $A.X + $t * $dx; $y = $A.Y + $t * $dy
    return [math]::Sqrt([math]::Pow($Point.X - $x, 2) + [math]::Pow($Point.Y - $y, 2))
}

function Get-DistanceToPolygon($Point, $Points) {
    if (-not $Points -or $Points.Count -lt 3) { return [double]::PositiveInfinity }
    if (Test-PointInPolygon $Point $Points) { return 0.0 }
    $min = [double]::PositiveInfinity
    for ($i = 0; $i -lt $Points.Count - 1; $i++) {
        $d = Get-DistanceToSegment $Point $Points[$i] $Points[$i + 1]
        if ($d -lt $min) { $min = $d }
    }
    return $min
}

$filesActual = @{
    "Baza Danych RCN Poznan 2021-2025.gml" = "Baza Danych RCN Pozna$([char]324) 2021-2025.gml"
    "Baza Danych RCN Poznan 2025.gml" = "Baza Danych RCN Pozna$([char]324) 2025.gml"
    "Baza Danych RCN Poznan 2026.gml" = "Baza Danych RCN Pozna$([char]324) 2026.gml"
    "Baza Danych RCN Poznan 2026-old.gml" = "Baza Danych RCN Pozna$([char]324) 2026-old.gml"
}

$features = [System.Collections.Generic.List[object]]::new()
foreach ($file in $Pliki) {
    $actualName = $filesActual[$file]
    if (-not $actualName) { $actualName = $file }
    $path = Join-Path $KatalogDanych $actualName
    if (-not (Test-Path -LiteralPath $path)) { Write-Warning "Brak pliku: $path"; continue }
    Write-Host "Czytam $actualName ..."
    $readerSettings = [System.Xml.XmlReaderSettings]::new()
    $readerSettings.DtdProcessing = [System.Xml.DtdProcessing]::Prohibit
    $readerSettings.IgnoreWhitespace = $true
    $reader = [System.Xml.XmlReader]::Create($path, $readerSettings)
    $count = 0
    try {
        while ($reader.Read()) {
            if ($reader.NodeType -eq [System.Xml.XmlNodeType]::Element -and $reader.LocalName -like 'RCN_*') {
                $element = [System.Xml.Linq.XElement]::ReadFrom($reader)
                $gmlId = $element.Attributes() | Where-Object { $_.Name.LocalName -eq 'id' } | Select-Object -First 1
                $id = if ($gmlId) { Strip-Timestamp $gmlId.Value } else { "" }
                $refs = [System.Collections.Generic.List[object]]::new()
                foreach ($node in $element.Descendants()) {
                    $href = $node.Attributes() | Where-Object { $_.Name.LocalName -eq 'href' } | Select-Object -First 1
                    if ($href) { $refs.Add([pscustomobject]@{ Kind = $node.Name.LocalName; Id = (Strip-Timestamp $href.Value) }) }
                }
                $features.Add([pscustomobject]@{
                    Type = $element.Name.LocalName
                    Id = $id
                    Element = $element
                    Refs = $refs.ToArray()
                    Points = (Get-PolygonPoints $element)
                    Source = $actualName
                })
                $count++
                if ($count % 25000 -eq 0) { Write-Host "  odczytano $count obiektow" }
            }
        }
    } finally { $reader.Dispose() }
    Write-Host "  obiektow: $count"
}

$seed = $features | Where-Object { $_.Type -eq 'RCN_Dzialka' -and (Get-DescendantText $_.Element 'idDzialki') -eq $DzialkaPunktu } | Select-Object -First 1
if (-not $seed) { throw "Nie znaleziono działki punktu: $DzialkaPunktu w podanych plikach." }
$center = Get-PolygonCentroid $seed.Points
Write-Host ("Point centroid EPSG:2177 X={0:N2}, Y={1:N2}; radius {2} m" -f $center.X, $center.Y, $PromienMetry)

$nearby = [System.Collections.Generic.List[object]]::new()
foreach ($feature in $features) {
    if ($feature.Points.Count -lt 3) { continue }
    $distance = Get-DistanceToPolygon $center $feature.Points
    if ($distance -le $PromienMetry) {
        $feature | Add-Member -NotePropertyName DistanceM -NotePropertyValue $distance -Force
        $nearby.Add($feature)
    }
}
$nearbyIds = @{}
foreach ($feature in $nearby) { if ($feature.Id) { $nearbyIds[$feature.Id] = $feature } }
$byId = @{}
foreach ($feature in $features) { if ($feature.Id) { $byId[$feature.Id] = $feature } }

$transactions = [System.Collections.Generic.List[object]]::new()
foreach ($tx in ($features | Where-Object { $_.Type -eq 'RCN_Transakcja' })) {
    $matched = [System.Collections.Generic.List[object]]::new()
    $relatedParcels = [System.Collections.Generic.List[string]]::new()
    $propertyAreasM2 = [System.Collections.Generic.List[double]]::new()
    $acquiredAreasM2 = [System.Collections.Generic.List[double]]::new()
    $propertyShares = [System.Collections.Generic.List[string]]::new()
    foreach ($nierRef in ($tx.Refs | Where-Object { $_.Kind -eq 'nieruchomosc' })) {
        $property = $byId[$nierRef.Id]
        if (-not $property) { continue }
        $areaText = Get-DescendantText $property.Element 'polePowierzchniNieruchomosciGruntowej'
        $shareText = Get-DescendantText $property.Element 'udzialWPrawieDoNieruchomosci'
        if ($shareText -and -not $propertyShares.Contains($shareText)) { $propertyShares.Add($shareText) }
        if ($areaText) {
            $areaM2 = [double]::Parse($areaText, $culture) * 10000
            $propertyAreasM2.Add($areaM2)
            if ($shareText -match '^(\d+)\/(\d+)$' -and [double]$Matches[2] -gt 0) {
                $acquiredAreasM2.Add($areaM2 * ([double]$Matches[1] / [double]$Matches[2]))
            }
        }
        foreach ($assetRef in $property.Refs | Where-Object { $_.Kind -in @('dzialka','budynek','lokal') }) {
            if ($nearbyIds.ContainsKey($assetRef.Id)) { $matched.Add($nearbyIds[$assetRef.Id]) }
            $asset = $byId[$assetRef.Id]
            if ($asset -and $asset.Type -eq 'RCN_Dzialka') {
                $parcelNo = Get-DescendantText $asset.Element 'idDzialki'
                if ($parcelNo -and -not $relatedParcels.Contains($parcelNo)) { $relatedParcels.Add($parcelNo) }
            }
        }
    }
    if ($matched.Count -eq 0) { continue }
    $doc = $null
    foreach ($docRef in ($tx.Refs | Where-Object { $_.Kind -eq 'podstawaPrawna' })) { if ($byId[$docRef.Id]) { $doc = $byId[$docRef.Id]; break } }
    $matchedUnique = @($matched | Sort-Object Id -Unique)
    $addresses = [System.Collections.Generic.List[string]]::new()
    foreach ($asset in $matchedUnique) {
        $street = Get-DescendantText $asset.Element 'ulica'
        $number = Get-DescendantText $asset.Element 'numerPorzadkowy'
        if ($street) {
            $address = if ($number) { "$street $number" } else { $street }
            if (-not $addresses.Contains($address)) { $addresses.Add($address) }
        }
    }
    $minDistance = ($matchedUnique | Measure-Object DistanceM -Minimum).Minimum
    $areaTotalM2 = if ($propertyAreasM2.Count) { ($propertyAreasM2 | Measure-Object -Sum).Sum } else { $null }
    $areaAcquiredM2 = if ($acquiredAreasM2.Count -eq $propertyAreasM2.Count -and $propertyAreasM2.Count) { ($acquiredAreasM2 | Measure-Object -Sum).Sum } else { $null }
    $transactionPrice = Get-DescendantText $tx.Element 'cenaTransakcjiBrutto'
    $pricePerM2 = $null
    if ($transactionPrice -and $areaAcquiredM2 -gt 0) {
        $pricePerM2 = [math]::Round(([double]::Parse($transactionPrice, $culture) / $areaAcquiredM2), 2)
    }
    $transactions.Add([pscustomobject]@{
        DataAktu = if ($doc) { Get-DescendantText $doc.Element 'dataSporzadzeniaDokumentu' } else { $null }
        NumerTransakcji = Get-DescendantText $tx.Element 'oznaczenieTransakcji'
        RodzajTransakcji = Get-DescendantText $tx.Element 'rodzajTransakcji'
        RodzajRynku = Get-DescendantText $tx.Element 'rodzajRynku'
        CenaTransakcjiPLN = $transactionPrice
        PowierzchniaGruntuM2 = if ($null -ne $areaTotalM2) { [math]::Round($areaTotalM2, 2) } else { $null }
        PowierzchniaUdzialuNabytegoM2 = if ($null -ne $areaAcquiredM2) { [math]::Round($areaAcquiredM2, 2) } else { $null }
        Udzialy = ($propertyShares -join '; ')
        CenaZaM2GruntuPLN = $pricePerM2
        Adresy = ($addresses -join '; ')
        DzialkiPowiazane = ($relatedParcels -join '; ')
        ObiektyWPromieniu = (($matchedUnique | ForEach-Object { "$($_.Type):$($_.Id)" }) -join '; ')
        OdlegloscNajblizszegoObiektuM = [math]::Round($minDistance, 1)
        PlikZrodlowy = $tx.Source
        TxId = $tx.Id
    })
}

$nearbyRows = foreach ($feature in ($nearby | Sort-Object DistanceM)) {
    $street = Get-DescendantText $feature.Element 'ulica'
    $number = Get-DescendantText $feature.Element 'numerPorzadkowy'
    $areaText = Get-DescendantText $feature.Element 'polePowierzchniEwidencyjnej'
    $parcelPriceText = Get-DescendantText $feature.Element 'cenaDzialkiEwidencyjnejBrutto'
    $parcelAreaM2 = if ($areaText) { [double]::Parse($areaText, $culture) * 10000 } else { $null }
    $parcelPrice = if ($parcelPriceText) { [double]::Parse($parcelPriceText, $culture) } else { $null }
    [pscustomobject]@{
        TypObiektu = $feature.Type
        IdGML = $feature.Id
        IdDzialki = if ($feature.Type -eq 'RCN_Dzialka') { Get-DescendantText $feature.Element 'idDzialki' } else { $null }
        Ulica = $street
        Numer = $number
        PowierzchniaEwidencyjnaM2 = if ($null -ne $parcelAreaM2) { [math]::Round($parcelAreaM2, 2) } else { $null }
        CenaDzialkiPLN = $parcelPrice
        CenaDzialkiZaM2PLN = if ($parcelPrice -and $parcelAreaM2 -gt 0) { [math]::Round(($parcelPrice / $parcelAreaM2), 2) } else { $null }
        Geometria2177 = ConvertTo-Json -InputObject @($feature.Points | ForEach-Object { ,@($_.X, $_.Y) }) -Compress -Depth 4
        OdlegloscOdSrodkaDzialkiPunktowejM = [math]::Round($feature.DistanceM, 1)
        PlikZrodlowy = $feature.Source
    }
}

$outTransactions = Join-Path $KatalogDanych "$PrefiksWyjscia-transakcje.csv"
$outFeatures = Join-Path $KatalogDanych "$PrefiksWyjscia-obiekty.csv"
$transactions | Sort-Object DataAktu, TxId -Unique | Export-Csv -LiteralPath $outTransactions -Delimiter ';' -NoTypeInformation -Encoding UTF8
$nearbyRows | Export-Csv -LiteralPath $outFeatures -Delimiter ';' -NoTypeInformation -Encoding UTF8

Write-Host ""
Write-Host "Obiekty geometryczne w promieniu: $($nearby.Count)"
Write-Host "Transakcje powiązane z obiektami w promieniu: $($transactions.Count)"
Write-Host "Transakcje CSV: $outTransactions"
Write-Host "Obiekty CSV: $outFeatures"
Write-Host "Centroid punktu (EPSG:2177): X=$($center.X); Y=$($center.Y)"
