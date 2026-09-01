# CWA 天氣現象代碼（WeatherCode）對照表

前端天氣圖示對應的參考資料。資料來源為中央氣象署官網天氣圖示對照表所使用的官方
資料檔 <https://www.cwa.gov.tw/Data/js/WeatherIcon.js>（原始檔共 353 筆描述、41 個
相異代碼），由本專案於 2026-09-01 擷取整理。機器可讀版本見 `weather-icons.json`。

## 重要前提

1. **以 `WeatherCode` 作為圖示對應的 key，不要用天氣描述文字。**
   官方同一個代碼底下最多對應到 41 種不同的中文描述（例如 code 32），
   用文字當 key 會讓對應表隨氣象署文案調整不斷破版。

2. **代碼不連續：實際為 1–39、41、42，共 41 個，沒有 code 40。**
   坊間常說的「1–41」其實是指「41 個代碼」，若用 `for (i = 1; i <= 41; i++)`
   產生對應表會漏掉 42（下雪）並多出不存在的 40。

3. **圖示檔名為兩位數補零**（`01.svg` ~ `42.svg`），且分日夜兩套：
   - 白天：`https://www.cwa.gov.tw/V8/assets/img/weather_icons/weathers/svg_icon/day/{code}.svg`
   - 夜晚：`https://www.cwa.gov.tw/V8/assets/img/weather_icons/weathers/svg_icon/night/{code}.svg`

   > 這是氣象署官網的靜態資源路徑，非開放資料 API 的一部分。正式上線建議把
   > SVG 下載後自行 host，避免外部路徑異動或熱連結問題。

## 完整對照表

| WeatherCode | 圖示檔名 | 代表描述 | 英文 | 描述變體數 |
|---:|---|---|---|---:|
| 1 | `01.svg` | 晴天 | Clear | 1 |
| 2 | `02.svg` | 晴時多雲 | Mostly Clear | 1 |
| 3 | `03.svg` | 多雲時晴 | Partly Clear | 1 |
| 4 | `04.svg` | 多雲 | Partly Cloudy | 1 |
| 5 | `05.svg` | 多雲時陰 | Mostly Cloudy | 1 |
| 6 | `06.svg` | 陰時多雲 | Mostly Cloudy | 1 |
| 7 | `07.svg` | 陰天 | Cloudy | 1 |
| 8 | `08.svg` | 多雲陣雨 | Partly Cloudy With Showers | 10 |
| 9 | `09.svg` | 多雲時陰短暫雨 | Mostly Cloudy With Occasional Rain | 2 |
| 10 | `10.svg` | 陰時多雲短暫雨 | Mostly Cloudy With Occasional Rain | 2 |
| 11 | `11.svg` | 雨天 | Rainy | 6 |
| 12 | `12.svg` | 多雲時陰有雨 | Mostly Cloudy With Rain | 4 |
| 13 | `13.svg` | 陰時多雲有雨 | Mostly Cloudy With Rain | 3 |
| 14 | `14.svg` | 陰有雨 | Rainy | 7 |
| 15 | `15.svg` | 多雲陣雨或雷雨 | Partly Cloudy With Showers Or Thundershowers | 11 |
| 16 | `16.svg` | 多雲時陰陣雨或雷雨 | Partly Cloudy With Showers Or Thunderstorms | 7 |
| 17 | `17.svg` | 陰時多雲有雷陣雨 | Mostly Cloudy With Thundershowers | 5 |
| 18 | `18.svg` | 陰有陣雨或雷雨 | Cloudy With Showers Or Thunderstorms | 18 |
| 19 | `19.svg` | 晴午後多雲局部雨 | Clear Becoming Partly Cloudy With Local Rain In The Afternoon | 14 |
| 20 | `20.svg` | 多雲午後局部雨 | Partly Cloudy With Local Afternoon Rain | 10 |
| 21 | `21.svg` | 晴午後多雲陣雨或雷雨 | Clear Becoming Partly Cloudy With Showers Or Thunderstorms In The Afternoon | 16 |
| 22 | `22.svg` | 多雲午後局部陣雨或雷雨 | Partly Cloudy With Local Afternoon Showers Or Thunderstorms | 13 |
| 23 | `23.svg` | 多雲局部陣雨或雪 | Partly Cloudy With Local Showers Or Snow | 38 |
| 24 | `24.svg` | 晴有霧 | Clear With Fog | 2 |
| 25 | `25.svg` | 晴時多雲有霧 | Mostly Clear With Fog | 2 |
| 26 | `26.svg` | 多雲時晴有霧 | Partly Clear With Fog | 2 |
| 27 | `27.svg` | 多雲有霧 | Partly Cloudy With Fog | 4 |
| 28 | `28.svg` | 陰有霧 | Cloudy With Fog | 6 |
| 29 | `29.svg` | 多雲局部雨 | Partly Cloudy With Local Rain | 4 |
| 30 | `30.svg` | 多雲時陰局部雨 | Mostly Cloudy With Local Rain | 16 |
| 31 | `31.svg` | 多雲有霧有局部雨 | Partly Cloudy With Fog And Local Rain | 22 |
| 32 | `32.svg` | 多雲時陰有霧有局部雨 | Mostly Cloudy With Fog And Local Rain | 41 |
| 33 | `33.svg` | 多雲局部陣雨或雷雨 | Partly Cloudy With Local Showers Or Thundershowers | 4 |
| 34 | `34.svg` | 多雲時陰局部陣雨或雷雨 | Partly Cloudy With Local Showers Or Thundershowers | 16 |
| 35 | `35.svg` | 多雲有陣雨或雷雨有霧 | Partly Cloudy With Showers Or Thunderstorms And Fog | 13 |
| 36 | `36.svg` | 多雲時陰有陣雨或雷雨有霧 | Mostly Cloudy With Showers Or Thunderstorms And Fog | 31 |
| 37 | `37.svg` | 多雲局部雨或雪有霧 | Partly Cloudy With Local Rain Or Snow And Fog | 6 |
| 38 | `38.svg` | 短暫陣雨有霧 | Occasional Showers With Fog | 4 |
| 39 | `39.svg` | 有雨有霧 | Rain With Fog | 2 |
| 41 | `41.svg` | 短暫陣雨或雷雨有霧 | Occasional Showers Or Thunderstorms With Fog | 2 |
| 42 | `42.svg` | 下雪 | Snow | 3 |
## observations 表無法沿用此表

`observations`（觀測資料）的 `weather` 欄位來自氣象署自動氣象站的觀測資料，
與本文件的預報詞彙是兩套獨立系統，不能共用同一張對應表：

- 觀測 API 回傳的 `raw_json` **不含 WeatherCode**，只有天氣現象文字，
  因此無法沿用上表以代碼對應的做法。
- 觀測詞彙的組成規則為 `{晴｜多雲｜陰}` ＋ 選配的 `{有雨｜有雷｜有雷雨}`，
  例如「多雲有雷雨」。
- 兩套詞彙不相容：官方預報表用的是「晴天」「陰天」，觀測回傳的則是簡寫的
  「晴」「陰」，直接拿文字去比對預報表會對不到。

觀測資料的圖示請依上述組成規則另建一張自訂對應表，並同樣保留未知值的 fallback。
