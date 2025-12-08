# Price Monitor Plugin (Sample External Plugin)

This document defines the **Price Monitor Plugin**, a non-core (optional) example plugin designed for user documentation, testing, and demonstration purposes.
It tracks **price changes** for URLs that match user-defined patterns and alerts the user when a price drops by a configurable percentage threshold.

This plugin showcases how ContextHelp plugins can extend the ingestion and refresh ecosystem by defining additional behaviors, adding domain-specific logic, and integrating with refresh schedules.

---

## Overview

The Price Monitor Plugin allows users to:

- Track prices for products from websites that match configurable regex patterns
- Extract price values during ingestion or refresh
- Monitor price changes over time
- Configure alert thresholds per bookmark or globally
- Receive alerts when returning to ContextHelp (e.g., on next CLI command, extension use, API call)
- Integrate seamlessly with the **Refresh Plugin** to require periodic refreshing for monitored sources

This plugin is not shipped with ContextHelp by default, but serves as a **reference implementation**.

---

## Motivation

Many users save products, courses, or subscriptions they want to purchase later:

- A laptop they’re monitoring on sale
- A SaaS product with seasonal discounts
- An online course whose price fluctuates
- A camera lens that occasionally dips in price

Users need:

- automated refresh
- consistent price tracking
- reliable alerts when prices fall
- a safe, opt-in mechanism for price monitoring

This plugin demonstrates how a domain-specific workload can integrate with ContextHelp’s ingestion, refresh, and alerting capabilities.

---

## Features

- Regex-based selection of URLs to monitor
- Automatic extraction of price from loaded content
- Integration with the Refresh Plugin for scheduled updates
- Per-bookmark or global price-drop thresholds
- Configurable alerts shown on next system interaction
- Price history storage
- Alert deduplication
- User-controlled opt-in for price tracking

---

## Configuration Model

### Global Configuration

```
price_monitor:
  enabled: true
  default_threshold_percent: 5
  patterns:
    - match: "amazon[.]com/.+"
      extract_regex: "\\$([0-9]+[.][0-9]{2})"
    - match: "store[.]example[.]com/product/.+"
      extract_regex: "price:\\s*([0-9]+)"
  require_refresh: true
  min_refresh_interval_seconds: 3600
```

### Bookmark-Level Overrides

```
"price_monitor": {
  "enabled": true,
  "threshold_percent": 10,
  "extract_regex": "\\$([0-9]+[.][0-9]{2})"
}
```

### CLI Flags

```
--price-monitor
--price-threshold 12
--price-regex "\\$([0-9]+[.][0-9]{2})"
```

Example CLI usage:

```
ch analyze https://amazon.com/product123 --price-monitor --price-threshold 10
```

### REST Example

```
POST /analyze
{
  "url": "https://amazon.com/product123",
  "price_monitor": {
    "enabled": true,
    "threshold_percent": 8
  }
}
```

---

## Price Extraction

The plugin attempts to extract a numerical price from:

- HTML content
- embedded JSON
- structured metadata (OpenGraph, LD+JSON, microformats)

It uses:

1. regex provided by the user (highest priority)
2. regex from global config
3. fallback heuristics (optional depending on implementation)

Example extraction patterns:

```
\\$([0-9]+[.][0-9]{2})
"price":\s*([0-9]+[.][0-9]{2})
"current_price":\s*([0-9]+)
```

---

## Refresh Integration

Because prices change over time, the plugin may require certain URLs to be refreshed periodically.

### Forcing Refresh Requirements

If a URL matches a pattern in `price_monitor.patterns`:

- The plugin asks Refresh Plugin to ensure:
  - `refresh.enabled = true`
  - Interval is not below `min_refresh_interval_seconds`
- If user opts in, refresh rules are auto-applied.

Example:

```
Pattern matched: amazon.com/product123
Refresh interval forced: 3600 seconds (or user override)
```

Opt-out is always respected (`--price-monitor=false` or `"enabled": false`).

### Refresh Cycle

During each refresh job:

1. Fetch new page
2. Extract updated price
3. Compare to last known price
4. If drop exceeds threshold:
   - Create an **alert entry**
   - Update bookmark metadata
5. Append to price history array

---

## Price History Storage

Example bookmark schema extension:

```
"price_history": [
  {
    "timestamp": 1710000000,
    "price": 1299.99
  },
  {
    "timestamp": 1710050000,
    "price": 1199.00
  }
]
```

Latest price stored as:

```
"price_monitor": {
  "current_price": 1199.00,
  "threshold_percent": 10,
  "last_alert_timestamp": null
}
```

---

## Alert Logic

### Trigger Condition

A drop alert is triggered when:

```
(previous_price - current_price) / previous_price * 100 >= threshold_percent
```

### Alert Delivery

Alerts are delivered **on next interaction** with ContextHelp:

- next `ch` CLI command
- next use of browser extension
- next REST/gRPC request

Alerts are shown in non-disruptive notification format.

Examples:

**CLI:**

```
Price Alert: amazon.com/product123 dropped from $1299.99 to $1199.00 (-7.7%).
```

**REST:**

```
"alerts": [
  {
    "url": "...",
    "old_price": 1299.99,
    "new_price": 1199.00,
    "percent_change": 7.7
  }
]
```

---

## API Integration

### CLI

```
ch price ls
ch price history <bookmark-id>
ch price alerts
```

### REST

```
GET /price/alerts
GET /price/history/:id
```

### gRPC

```
rpc GetPriceAlerts(PriceAlertRequest) returns (PriceAlertResponse);
```

---

## Security & Permissioning

- Price monitoring is **opt-in only**.
- Plugin must request permission to:
  - enable refresh
  - fetch product pages periodically
- Users may disable monitoring at any time.
- No financial information is handled.
- Only public product URLs are allowed unless user config explicitly allows private endpoints.

---

## Performance Considerations

- Price extraction is cheap; HTML is already fetched by pipeline.
- Refresh intervals must respect system-wide constraints.
- Alert list must be small and bounded (auto-prune old entries).

---

## Use Cases

### 1. Track Amazon product pricing
Notify when a laptop drops below configured threshold.

### 2. Monitor SaaS price changes
Changes in plan costs or promotional discounts.

### 3. Check for seasonal discounts
Holiday sales, special events.

### 4. Monitor multiple stores with regex
Different patterns for each site.

---

## Future Extensions

- Multi-currency normalization
- Price prediction
- Machine-learned pricing thresholds
- Vendor API integration (e.g., Amazon Product API)
- Webhook or push notifications

---

## Summary

The Price Monitor Plugin is a sample, non-core extension that demonstrates:

- How plugins can integrate with the **Refresh Plugin**
- How domain-specific extraction logic works
- How alert systems operate inside ContextHelp
- How bookmark-level enrichment can be extended safely

It shows the power of extending ContextHelp’s ingestion, refresh, and semantic graph subsystems with user-defined domain workflows.