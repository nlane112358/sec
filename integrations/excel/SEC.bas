Attribute VB_Name = "SECModule"
' ==============================================================================
' SEC EDGAR Fundamentals & Valuation Add-In for Microsoft Excel
' Compatible with Excel on Windows & macOS (Excel 365, 2021, 2019, 2016)
'
' Instructions:
' 1. In Excel, press Alt+F11 (or Fn+Option+F11 on Mac) to open the VBA Editor.
' 2. Go to File > Import File... and select this SEC.bas file.
' 3. Close the VBA Editor.
' 4. In any cell, use:
'       =SEC("MSFT", "ADJ_ROIC")
'       =SEC("AAPL", "REVENUE", "TTM")
'       =SEC("NVDA", "FCF")
' ==============================================================================

Private Const SEC_SERVER As String = "http://localhost:8080"

Public Function SEC(ByVal ticker As String, ByVal metric As String, Optional ByVal period As String = "TTM", Optional ByVal raw As Boolean = True) As Variant
    Dim http As Object
    Dim url As String
    Dim responseText As String
    Dim numVal As Double
    
    ticker = Trim(UCase(ticker))
    metric = Trim(UCase(metric))
    period = Trim(UCase(period))
    If period = "" Then period = "TTM"
    
    If ticker = "" Or metric = "" Then
        SEC = CVErr(xlErrValue)
        Exit Function
    End If
    
    url = SEC_SERVER & "/api/sec/" & ticker & "/" & metric & "?period=" & period & "&raw=" & IIf(raw, "1", "0")
    
    On Error GoTo ErrorHandler
    
    #If Mac Then
        ' macOS VBA HTTP via curl
        Dim script As String
        script = "curl -s -m 5 """ & url & """"
        responseText = MacScript("do shell script """ & script & """")
    #Else
        ' Windows VBA HTTP via MSXML2
        Set http = CreateObject("MSXML2.ServerXMLHTTP.6.0")
        http.Open "GET", url, False
        http.setRequestHeader "Accept", "text/plain"
        http.Send
        If http.Status <> 200 Then
            SEC = CVErr(xlErrNA)
            Exit Function
        End If
        responseText = http.responseText
    #End If
    
    responseText = Trim(responseText)
    
    If raw Then
        If IsNumeric(responseText) Then
            SEC = CDbl(responseText)
            Exit Function
        End If
    End If
    
    SEC = responseText
    Exit Function

ErrorHandler:
    SEC = CVErr(xlErrNA)
End Function
