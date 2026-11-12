/**
 * SEC EDGAR Fundamentals & Valuation Add-In for Google Sheets
 * 
 * Instructions:
 * 1. In Google Sheets, go to Extensions > Apps Script
 * 2. Delete any existing code in Code.gs, paste this entire file, and click Save (⌘S / Ctrl+S)
 * 3. Return to your spreadsheet. Use =SEC("MSFT", "ADJ_ROIC") in any cell!
 *
 * Default endpoint connects to your local SEC server at http://localhost:8080 (or your hosted SEC server).
 */

const SEC_CONFIG = {
  // Change this if running SEC on a custom host, domain, or port:
  SERVER_URL: 'http://localhost:8080',
  CACHE_EXPIRATION_SECONDS: 21600, // 6 hours
};

/**
 * Retrieves fundamental SEC EDGAR data for any company.
 *
 * @param {"MSFT"} ticker Stock ticker (e.g. "MSFT", "AAPL", "NVDA", "TSM")
 * @param {"ADJ_ROIC"} metric Metric code (e.g. "ADJ_ROIC", "REVENUE", "FCF", "NET_INCOME", "OPERATING_INCOME", "PE", "SHARES")
 * @param {"TTM"} period Optional period: "TTM" (default), "FY", "2024", "2023", "Q3-2024", or "0" for latest
 * @param {true} raw Optional: true for numeric output (default), false for formatted string (e.g., "$245.1B", "34.2%")
 * @return {number|string} Fundamental metric value
 * @customfunction
 */
function SEC(ticker, metric, period, raw) {
  if (!ticker || !metric) {
    return "";
  }
  
  ticker = String(ticker).trim().toUpperCase();
  metric = String(metric).trim().toUpperCase();
  period = period ? String(period).trim() : "TTM";
  if (raw === undefined || raw === null || raw === "") {
    raw = true;
  }
  
  var cacheKey = "SEC_" + ticker + "_" + metric + "_" + period + "_" + (raw ? "RAW" : "TXT");
  var cache = CacheService.getScriptCache();
  var cached = cache.get(cacheKey);
  if (cached !== null) {
    return raw ? Number(cached) : cached;
  }
  
  var url = SEC_CONFIG.SERVER_URL + "/api/sec/" + encodeURIComponent(ticker) + "/" + encodeURIComponent(metric) +
            "?period=" + encodeURIComponent(period) + "&raw=" + (raw ? "1" : "0");
            
  try {
    var response = UrlFetchApp.fetch(url, {
      muteHttpExceptions: true,
      headers: { "Accept": "text/plain" }
    });
    
    var code = response.getResponseCode();
    if (code !== 200) {
      return "#ERR: " + response.getContentText();
    }
    
    var content = response.getContentText().trim();
    if (raw) {
      var num = parseFloat(content);
      if (!isNaN(num)) {
        cache.put(cacheKey, String(num), SEC_CONFIG.CACHE_EXPIRATION_SECONDS);
        return num;
      }
    }
    
    cache.put(cacheKey, content, SEC_CONFIG.CACHE_EXPIRATION_SECONDS);
    return content;
  } catch (e) {
    return "#ERR: " + e.message;
  }
}

/**
 * High-speed batch lookup across multiple companies and metrics in a single API call.
 * Spills a 2D matrix of fundamental values into your sheet.
 *
 * @param {A2:A20} tickers Range or column of stock tickers (e.g., A2:A20)
 * @param {B1:F1} metrics Range or row of metrics (e.g., B1:F1)
 * @param {"TTM"} period Optional period: "TTM" (default) or "FY"
 * @param {true} raw Optional: true for numeric values (default), false for formatted text
 * @return {Array<Array<number|string>>} 2D table of fundamental data
 * @customfunction
 */
function SEC_MATRIX(tickers, metrics, period, raw) {
  if (!tickers || !metrics) {
    return "";
  }
  
  // Flatten tickers array
  var tickerList = [];
  if (Array.isArray(tickers)) {
    for (var i = 0; i < tickers.length; i++) {
      var row = tickers[i];
      if (Array.isArray(row)) {
        for (var j = 0; j < row.length; j++) {
          var t = String(row[j]).trim().toUpperCase();
          if (t && tickerList.indexOf(t) === -1) tickerList.push(t);
        }
      } else {
        var t = String(row).trim().toUpperCase();
        if (t && tickerList.indexOf(t) === -1) tickerList.push(t);
      }
    }
  } else {
    tickerList.push(String(tickers).trim().toUpperCase());
  }
  
  // Flatten metrics array
  var metricList = [];
  if (Array.isArray(metrics)) {
    for (var i = 0; i < metrics.length; i++) {
      var row = metrics[i];
      if (Array.isArray(row)) {
        for (var j = 0; j < row.length; j++) {
          var m = String(row[j]).trim().toUpperCase();
          if (m && metricList.indexOf(m) === -1) metricList.push(m);
        }
      } else {
        var m = String(row).trim().toUpperCase();
        if (m && metricList.indexOf(m) === -1) metricList.push(m);
      }
    }
  } else {
    metricList.push(String(metrics).trim().toUpperCase());
  }
  
  period = period ? String(period).trim() : "TTM";
  if (raw === undefined || raw === null) raw = true;
  
  var url = SEC_CONFIG.SERVER_URL + "/api/sec/batch?tickers=" + encodeURIComponent(tickerList.join(',')) +
            "&metrics=" + encodeURIComponent(metricList.join(',')) +
            "&period=" + encodeURIComponent(period);
            
  try {
    var response = UrlFetchApp.fetch(url, { muteHttpExceptions: true });
    if (response.getResponseCode() !== 200) {
      return "#ERR: Batch request failed";
    }
    var json = JSON.parse(response.getContentText());
    return raw ? json.values : json.formatted;
  } catch (e) {
    // Fallback: evaluate element-by-element
    var result = [];
    for (var r = 0; r < tickerList.length; r++) {
      var rowVals = [];
      for (var c = 0; c < metricList.length; c++) {
        rowVals.push(SEC(tickerList[r], metricList[c], period, raw));
      }
      result.push(rowVals);
    }
    return result;
  }
}
