param(
    [string]$KatalogDanych = $PSScriptRoot,
    [string]$PlikWyjsciowy = "mapa_transakcji_standalone.html"
)

$ErrorActionPreference = 'Stop'
$mapPath = Join-Path $KatalogDanych 'mapa_transakcji.html'
$parcelsPath = Join-Path $KatalogDanych 'okolica_2-38_500m-obiekty.csv'
$transactionsPath = Join-Path $KatalogDanych 'okolica_2-38_500m-transakcje.csv'
foreach ($path in @($mapPath, $parcelsPath, $transactionsPath)) {
    if (-not (Test-Path -LiteralPath $path)) { throw "Brak wymaganego pliku: $path" }
}

Write-Host 'Wczytuję pliki CSV ...'
Write-Host 'Loading CSV files ...'
$parcels = @(Import-Csv -LiteralPath $parcelsPath -Delimiter ';' -Encoding UTF8)
$transactions = @(Import-Csv -LiteralPath $transactionsPath -Delimiter ';' -Encoding UTF8)
$parcelsJson = ConvertTo-Json -InputObject $parcels -Depth 5 -Compress
$transactionsJson = ConvertTo-Json -InputObject $transactions -Depth 5 -Compress
$parcelsJson = $parcelsJson.Replace('<', '\u003c')
$transactionsJson = $transactionsJson.Replace('<', '\u003c')

$assets = @{
    LeafletCss = 'https://unpkg.com/leaflet@1.9.4/dist/leaflet.css'
    LeafletJs = 'https://unpkg.com/leaflet@1.9.4/dist/leaflet.js'
    Proj4Js = 'https://unpkg.com/proj4@2.11.0/dist/proj4.js'
}
$orthoBounds = '16.942,52.455,16.976,52.475'
$orthoUrl = "https://mapy.geoportal.gov.pl/wss/service/PZGIK/ORTO/WMS/StandardResolution?SERVICE=WMS&VERSION=1.1.1&REQUEST=GetMap&LAYERS=Raster&STYLES=&SRS=EPSG:4326&BBOX=$orthoBounds&WIDTH=1600&HEIGHT=1600&FORMAT=image/jpeg"
Write-Host 'Downloading Geoportal orthophoto ...'
$orthoResponse = Invoke-WebRequest -Uri $orthoUrl -UseBasicParsing -TimeoutSec 90
if ($orthoResponse.Headers['Content-Type'] -notlike 'image/jpeg*') { throw "Geoportal did not return a JPEG image: $($orthoResponse.Headers['Content-Type'])" }
$orthoBase64 = [Convert]::ToBase64String($orthoResponse.Content)
$overpassQuery = '[out:json][timeout:25];way[highway][name](52.455,16.942,52.475,16.976);out geom;'
Write-Host 'Downloading named streets from OpenStreetMap ...'
$osmResponse = Invoke-RestMethod -Uri 'https://overpass-api.de/api/interpreter' -Method Post -Body @{ data = $overpassQuery } -Headers @{ 'User-Agent' = 'RCNParcelMap/1.0 (local static map export)' } -TimeoutSec 60
$osmWays = @($osmResponse.elements | Where-Object { $_.type -eq 'way' -and $_.tags.name -and $_.geometry.Count -ge 2 } | ForEach-Object {
    [pscustomobject]@{
        name = [string]$_.tags.name
        geometry = @($_.geometry | Select-Object lon, lat)
    }
})
if ($osmWays.Count -eq 0) { throw 'OpenStreetMap returned no named streets for the map extent.' }
$osmWaysJson = ConvertTo-Json -InputObject $osmWays -Depth 6 -Compress
$osmWaysJson = $osmWaysJson.Replace('<', '\u003c')
Write-Host "Named street geometries: $($osmWays.Count)"
$downloaded = @{}
foreach ($name in $assets.Keys) {
    Write-Host "Embedding library $name ..."
    $response = Invoke-WebRequest -Uri $assets[$name] -UseBasicParsing -TimeoutSec 60
    $downloaded[$name] = [string]$response.Content
}
if ($downloaded.LeafletJs -match '</script\s*>|</SCRIPT\s*>') { throw 'Pobrany JavaScript zawiera znacznik kończący script; przerwano bezpieczny eksport.' }

$html = [System.IO.File]::ReadAllText($mapPath, [System.Text.Encoding]::UTF8)
$html = $html.Replace('<link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.4/dist/leaflet.css">', "<style>`n$($downloaded.LeafletCss)`n</style>")
$html = $html.Replace('<script src="https://unpkg.com/leaflet@1.9.4/dist/leaflet.js"></script>', "<script>`n$($downloaded.LeafletJs)`n</script>")
$html = $html.Replace('<script src="https://unpkg.com/proj4@2.11.0/dist/proj4.js"></script>', "<script>`n$($downloaded.Proj4Js)`n</script>")
$mapSetup = "const map=L.map('map',{preferCanvas:true,zoomControl:true}).setView([52.465,16.959],15);L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png',{maxZoom:20,attribution:'&copy; OpenStreetMap'}).addTo(map);"
$embeddedMapSetup = "const map=L.map('map',{preferCanvas:true,zoomControl:true}).setView([52.465,16.959],15);L.imageOverlay('data:image/jpeg;base64,$orthoBase64',[[52.455,16.942],[52.475,16.976]],{attribution:'Ortofotomapa &copy; GUGiK | &copy; OpenStreetMap contributors'}).addTo(map);"
$html = $html.Replace($mapSetup, $embeddedMapSetup)
$html = $html.Replace('</style>', '.actions{display:none!important}.standalone-note{display:block!important}@media(max-width:760px){html,body{width:100%;min-width:320px}.map-wrap{height:55vh;height:55dvh;min-height:320px}.map-overlay{top:8px;left:8px;right:8px;max-width:calc(100% - 16px);padding:9px 11px}.map-overlay p{font-size:10px}.map-filter{font-size:11px}.side{height:34vh;height:34dvh;min-height:240px}.bottom{grid-template-columns:minmax(0,1fr)}.detail-main,.detail-side{padding:12px}.street-label{font-size:10px}}.standalone-note{font-size:10px;line-height:1.4}</style>')
$html = $html.Replace('<div class="actions">', '<div class="standalone-note" style="display:none;font-size:11px;color:#64736a">Offline map with embedded orthophoto &copy; GUGiK.</div><div class="actions">')

$streetCode = @'
const osmStreetWays=__OSM_STREET_DATA__;
const streetLabels=[];
const longestStreetWay=new Map();
for(const way of osmStreetWays){let length=0;for(let i=1;i<way.geometry.length;i++){const a=way.geometry[i-1],b=way.geometry[i];length+=Math.hypot((Number(b.lon)-Number(a.lon))*Math.cos(Number(a.lat)*Math.PI/180),Number(b.lat)-Number(a.lat))*111320}const current=longestStreetWay.get(way.name);if(!current||length>current.length)longestStreetWay.set(way.name,{way,length})}
for(const [name,item] of longestStreetWay){if(item.length<65)continue;const pts=item.way.geometry;let total=0,segments=[];for(let i=1;i<pts.length;i++){const a=pts[i-1],b=pts[i],length=Math.hypot((Number(b.lon)-Number(a.lon))*Math.cos(Number(a.lat)*Math.PI/180),Number(b.lat)-Number(a.lat))*111320;segments.push({a,b,length});total+=length}let remaining=total/2,mid=pts[0];for(const segment of segments){if(remaining<=segment.length){const t=segment.length?remaining/segment.length:0;mid={lon:Number(segment.a.lon)+(Number(segment.b.lon)-Number(segment.a.lon))*t,lat:Number(segment.a.lat)+(Number(segment.b.lat)-Number(segment.a.lat))*t};break}remaining-=segment.length}const marker=L.circleMarker([mid.lat,mid.lon],{radius:1,opacity:0,fillOpacity:0,interactive:false}).bindTooltip(name,{permanent:true,direction:'center',className:'street-label'}).addTo(map);streetLabels.push(marker)}
'@.Replace('__OSM_STREET_DATA__', $osmWaysJson)
$embed = "`n// Embedded CSV and named street data.`nstate.parcels = $parcelsJson;`nstate.tx = $transactionsJson;`ndraw();`n$streetCode`n"
$html = $html.Replace('</style>', '.street-label{border:0;background:rgba(255,255,255,.84);color:#17291f;padding:2px 5px;border-radius:4px;box-shadow:0 1px 4px #18251c55;font:700 11px/1.2 Inter,system-ui,sans-serif;white-space:nowrap}.street-label:before{display:none}</style>')
$html = $html.Replace('Offline map with embedded orthophoto &copy; GUGiK.', 'Offline orthophoto &copy; GUGiK; street names &copy; OpenStreetMap contributors, ODbL.')
$closingScriptIndex = $html.LastIndexOf('</script>', [System.StringComparison]::OrdinalIgnoreCase)
if ($closingScriptIndex -lt 0) { throw 'Nie znaleziono końcowego skryptu mapy.' }
$html = $html.Insert($closingScriptIndex, $embed)

$outPath = Join-Path $KatalogDanych $PlikWyjsciowy
[System.IO.File]::WriteAllText($outPath, $html, [System.Text.UTF8Encoding]::new($true))
Write-Host "Utworzono: $outPath"
Write-Host "Embedded parcels: $($parcels.Count); transactions: $($transactions.Count)"
Write-Host 'No external basemap tiles are requested; parcel data and outlines are embedded.'
