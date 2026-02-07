# Chartly 2.0

**Production-ready contracts-first data platform. 9 microservices. Deploy in 60 seconds.**

## Crypto Profile Generator

Generate CoinGecko profiles (top-10 by default):

```powershell
# Generate YAML into ./profiles
C:\Chartly2.0\cmd\chartly\chartly.exe generate crypto --ids bitcoin,ethereum,cardano --timeframes live,1h,24h,7d --output ./profiles

# Optionally auto-register to the control plane (requires CHARTLY_API_KEY)
$env:CHARTLY_API_KEY = "<your-api-key>"
C:\Chartly2.0\cmd\chartly\chartly.exe generate crypto --register
```

Profiles created:
- `crypto-<id>-live`
- `crypto-<id>-1h`
- `crypto-<id>-24h`
- `crypto-<id>-7d`
- `crypto-top10-live`
