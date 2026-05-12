# Jurisdiction Registry Recipes

## Purpose

Map Canadian provincial/territorial and US state headquarters jurisdictions to
business registries, with a starter `kit/ibr` warmup recipe for each registry.

These recipes are seed contracts, not trusted facts by themselves. `kit/ibr`
uses them to browse, search, scrape, and detect breakage. `kit/eva` evaluates
source agreement and field confidence. Kit's `llm` extracts normalized candidate
facts and renders approval diffs.

## Recipe contract

```yaml
jurisdiction: US-DE
registry: Delaware Division of Corporations
search_url: https://icis.corp.delaware.gov/ecorp/entitysearch/namesearch.aspx
ibr_recipe:
  kind: business_registry_search
  query_example:
    name: "Acme"
  expected_fields:
    - legal_name
    - status
    - registry_id
    - registered_address
    - filing_date
  update_when:
    - search_form_not_found
    - no_results_for_known_fixture
    - expected_field_missing
    - captcha_or_block_detected
```

All samples below use `Acme` as a non-authoritative warmup query. Replace it
with a known fixture for production checks.

## Canada

| Jurisdiction | Registry | Search URL | `kit/ibr` warmup |
|---|---|---|---|
| `CA-AB` | Alberta Corporate Registry | `https://www.alberta.ca/corporate-registry` | `business_registry_search(name="Acme", jurisdiction="CA-AB")` |
| `CA-BC` | BC Registries and Online Services | `https://www.bcregistry.gov.bc.ca/` | `business_registry_search(name="Acme", jurisdiction="CA-BC")` |
| `CA-MB` | Manitoba Companies Office | `https://companiesoffice.gov.mb.ca/` | `business_registry_search(name="Acme", jurisdiction="CA-MB")` |
| `CA-NB` | New Brunswick Corporate Affairs Registry | `https://www2.snb.ca/content/snb/en/sites/corporate-registry.html` | `business_registry_search(name="Acme", jurisdiction="CA-NB")` |
| `CA-NL` | Newfoundland and Labrador Registry of Companies | `https://www.gov.nl.ca/dgsnl/registries/companies/` | `business_registry_search(name="Acme", jurisdiction="CA-NL")` |
| `CA-NS` | Nova Scotia Registry of Joint Stock Companies | `https://rjsc.novascotia.ca/` | `business_registry_search(name="Acme", jurisdiction="CA-NS")` |
| `CA-NT` | Northwest Territories Corporate Registries | `https://www.justice.gov.nt.ca/en/corporate-registries/` | `business_registry_search(name="Acme", jurisdiction="CA-NT")` |
| `CA-NU` | Nunavut Corporate Registries | `https://www.gov.nu.ca/edt/information/corporate-registries` | `business_registry_search(name="Acme", jurisdiction="CA-NU")` |
| `CA-ON` | Ontario Business Registry | `https://www.ontario.ca/page/ontario-business-registry` | `business_registry_search(name="Acme", jurisdiction="CA-ON")` |
| `CA-PE` | Prince Edward Island Corporate/Business Names Registry | `https://www.princeedwardisland.ca/en/topic/business-corporate-registry` | `business_registry_search(name="Acme", jurisdiction="CA-PE")` |
| `CA-QC` | Registraire des entreprises du Quebec | `https://www.registreentreprises.gouv.qc.ca/` | `business_registry_search(name="Acme", jurisdiction="CA-QC")` |
| `CA-SK` | Saskatchewan Corporate Registry | `https://www.isc.ca/CorporateRegistry` | `business_registry_search(name="Acme", jurisdiction="CA-SK")` |
| `CA-YT` | Yukon Corporate Affairs | `https://yukon.ca/en/doing-business/licensing/learn-about-corporate-affairs` | `business_registry_search(name="Acme", jurisdiction="CA-YT")` |

## United States

| Jurisdiction | Registry | Search URL | `kit/ibr` warmup |
|---|---|---|---|
| `US-AL` | Alabama Secretary of State Business Services | `https://arc-sos.state.al.us/CGI/CORPNAME.MBR/INPUT` | `business_registry_search(name="Acme", jurisdiction="US-AL")` |
| `US-AK` | Alaska Division of Corporations, Business and Professional Licensing | `https://www.commerce.alaska.gov/cbp/main/search/entities` | `business_registry_search(name="Acme", jurisdiction="US-AK")` |
| `US-AZ` | Arizona Corporation Commission eCorp | `https://ecorp.azcc.gov/EntitySearch/Index` | `business_registry_search(name="Acme", jurisdiction="US-AZ")` |
| `US-AR` | Arkansas Secretary of State Business Entity Search | `https://www.sos.arkansas.gov/corps/search_all.php` | `business_registry_search(name="Acme", jurisdiction="US-AR")` |
| `US-CA` | California Secretary of State BizFile Online | `https://bizfileonline.sos.ca.gov/search/business` | `business_registry_search(name="Acme", jurisdiction="US-CA")` |
| `US-CO` | Colorado Secretary of State Business Database | `https://www.sos.state.co.us/biz/BusinessEntityCriteriaExt.do` | `business_registry_search(name="Acme", jurisdiction="US-CO")` |
| `US-CT` | Connecticut Business Registry Search | `https://service.ct.gov/business/s/onlinebusinesssearch` | `business_registry_search(name="Acme", jurisdiction="US-CT")` |
| `US-DE` | Delaware Division of Corporations | `https://icis.corp.delaware.gov/ecorp/entitysearch/namesearch.aspx` | `business_registry_search(name="Acme", jurisdiction="US-DE")` |
| `US-FL` | Florida Division of Corporations Sunbiz | `https://search.sunbiz.org/Inquiry/CorporationSearch/ByName` | `business_registry_search(name="Acme", jurisdiction="US-FL")` |
| `US-GA` | Georgia Corporations Division | `https://ecorp.sos.ga.gov/BusinessSearch` | `business_registry_search(name="Acme", jurisdiction="US-GA")` |
| `US-HI` | Hawaii Business Express BREG | `https://hbe.ehawaii.gov/documents/search.html` | `business_registry_search(name="Acme", jurisdiction="US-HI")` |
| `US-ID` | Idaho Secretary of State Business Search | `https://sosbiz.idaho.gov/search/business` | `business_registry_search(name="Acme", jurisdiction="US-ID")` |
| `US-IL` | Illinois Secretary of State Business Search | `https://apps.ilsos.gov/businessentitysearch/` | `business_registry_search(name="Acme", jurisdiction="US-IL")` |
| `US-IN` | Indiana INBiz Business Search | `https://bsd.sos.in.gov/publicbusinesssearch` | `business_registry_search(name="Acme", jurisdiction="US-IN")` |
| `US-IA` | Iowa Secretary of State Business Entities Search | `https://sos.iowa.gov/search/business/search.aspx` | `business_registry_search(name="Acme", jurisdiction="US-IA")` |
| `US-KS` | Kansas Business Center Business Entity Search | `https://www.kansas.gov/bess/flow/main` | `business_registry_search(name="Acme", jurisdiction="US-KS")` |
| `US-KY` | Kentucky Secretary of State FastTrack | `https://web.sos.ky.gov/ftsearch/` | `business_registry_search(name="Acme", jurisdiction="US-KY")` |
| `US-LA` | Louisiana Secretary of State Business Filings Search | `https://coraweb.sos.la.gov/commercialsearch/commercialsearch.aspx` | `business_registry_search(name="Acme", jurisdiction="US-LA")` |
| `US-ME` | Maine Bureau of Corporations, Elections and Commissions | `https://icrs.informe.org/nei-sos-icrs/ICRS` | `business_registry_search(name="Acme", jurisdiction="US-ME")` |
| `US-MD` | Maryland Business Express Entity Search | `https://egov.maryland.gov/BusinessExpress/EntitySearch` | `business_registry_search(name="Acme", jurisdiction="US-MD")` |
| `US-MA` | Massachusetts Corporations Division Search | `https://corp.sec.state.ma.us/corpweb/CorpSearch/CorpSearch.aspx` | `business_registry_search(name="Acme", jurisdiction="US-MA")` |
| `US-MI` | Michigan LARA Business Entity Search | `https://cofs.lara.state.mi.us/SearchApi/Search/Search` | `business_registry_search(name="Acme", jurisdiction="US-MI")` |
| `US-MN` | Minnesota Secretary of State Business Search | `https://mblsportal.sos.state.mn.us/Business/Search` | `business_registry_search(name="Acme", jurisdiction="US-MN")` |
| `US-MS` | Mississippi Secretary of State Business Search | `https://corp.sos.ms.gov/corp/portal/c/page/corpBusinessIdSearch/portal.aspx` | `business_registry_search(name="Acme", jurisdiction="US-MS")` |
| `US-MO` | Missouri Secretary of State Business Entity Search | `https://bsd.sos.mo.gov/BusinessEntity/BESearch.aspx` | `business_registry_search(name="Acme", jurisdiction="US-MO")` |
| `US-MT` | Montana Secretary of State Business Search | `https://biz.sosmt.gov/search/business` | `business_registry_search(name="Acme", jurisdiction="US-MT")` |
| `US-NE` | Nebraska Secretary of State Corporate and Business Search | `https://www.nebraska.gov/sos/corp/corpsearch.cgi` | `business_registry_search(name="Acme", jurisdiction="US-NE")` |
| `US-NV` | Nevada SilverFlume Business Search | `https://esos.nv.gov/EntitySearch/OnlineEntitySearch` | `business_registry_search(name="Acme", jurisdiction="US-NV")` |
| `US-NH` | New Hampshire QuickStart Business Search | `https://quickstart.sos.nh.gov/online/BusinessInquire` | `business_registry_search(name="Acme", jurisdiction="US-NH")` |
| `US-NJ` | New Jersey Business Name Search | `https://www.njportal.com/DOR/BusinessNameSearch/Search/BusinessName` | `business_registry_search(name="Acme", jurisdiction="US-NJ")` |
| `US-NM` | New Mexico Secretary of State Business Search | `https://portal.sos.state.nm.us/BFS/online/CorporationBusinessSearch` | `business_registry_search(name="Acme", jurisdiction="US-NM")` |
| `US-NY` | New York Department of State Corporation and Business Entity Database | `https://apps.dos.ny.gov/publicInquiry/` | `business_registry_search(name="Acme", jurisdiction="US-NY")` |
| `US-NC` | North Carolina Secretary of State Business Registration Search | `https://www.sosnc.gov/online_services/search/by_title/_Business_Registration` | `business_registry_search(name="Acme", jurisdiction="US-NC")` |
| `US-ND` | North Dakota FirstStop Business Search | `https://firststop.sos.nd.gov/search/business` | `business_registry_search(name="Acme", jurisdiction="US-ND")` |
| `US-OH` | Ohio Secretary of State Business Search | `https://businesssearch.ohiosos.gov/` | `business_registry_search(name="Acme", jurisdiction="US-OH")` |
| `US-OK` | Oklahoma Secretary of State Business Search | `https://www.sos.ok.gov/corp/corpinquiryfind.aspx` | `business_registry_search(name="Acme", jurisdiction="US-OK")` |
| `US-OR` | Oregon Secretary of State Business Registry Search | `https://egov.sos.state.or.us/br/pkg_web_name_srch_inq.login` | `business_registry_search(name="Acme", jurisdiction="US-OR")` |
| `US-PA` | Pennsylvania Business Entity Search | `https://file.dos.pa.gov/search/business` | `business_registry_search(name="Acme", jurisdiction="US-PA")` |
| `US-RI` | Rhode Island Department of State Business Services Search | `https://business.sos.ri.gov/CorpWeb/CorpSearch/CorpSearch.aspx` | `business_registry_search(name="Acme", jurisdiction="US-RI")` |
| `US-SC` | South Carolina Secretary of State Business Entities Online | `https://businessfilings.sc.gov/BusinessFiling/Entity/Search` | `business_registry_search(name="Acme", jurisdiction="US-SC")` |
| `US-SD` | South Dakota Secretary of State Business Search | `https://sosenterprise.sd.gov/BusinessServices/Business/FilingSearch.aspx` | `business_registry_search(name="Acme", jurisdiction="US-SD")` |
| `US-TN` | Tennessee Secretary of State Business Information Search | `https://tnbear.tn.gov/Ecommerce/FilingSearch.aspx` | `business_registry_search(name="Acme", jurisdiction="US-TN")` |
| `US-TX` | Texas Comptroller Taxable Entity Search | `https://mycpa.cpa.state.tx.us/coa/` | `business_registry_search(name="Acme", jurisdiction="US-TX")` |
| `US-UT` | Utah Division of Corporations Business Search | `https://secure.utah.gov/bes/` | `business_registry_search(name="Acme", jurisdiction="US-UT")` |
| `US-VT` | Vermont Secretary of State Business Search | `https://bizfilings.vermont.gov/online/BusinessInquire` | `business_registry_search(name="Acme", jurisdiction="US-VT")` |
| `US-VA` | Virginia State Corporation Commission Clerk's Information System | `https://cis.scc.virginia.gov/EntitySearch/Index` | `business_registry_search(name="Acme", jurisdiction="US-VA")` |
| `US-WA` | Washington Corporations and Charities Filing System | `https://ccfs.sos.wa.gov/#/BusinessSearch` | `business_registry_search(name="Acme", jurisdiction="US-WA")` |
| `US-WV` | West Virginia Secretary of State Business Organization Search | `https://apps.wv.gov/SOS/BusinessEntitySearch/` | `business_registry_search(name="Acme", jurisdiction="US-WV")` |
| `US-WI` | Wisconsin Department of Financial Institutions Corporate Records | `https://www.wdfi.org/apps/CorpSearch/Search.aspx` | `business_registry_search(name="Acme", jurisdiction="US-WI")` |
| `US-WY` | Wyoming Secretary of State Business Center | `https://wyobiz.wyo.gov/Business/FilingSearch.aspx` | `business_registry_search(name="Acme", jurisdiction="US-WY")` |

## `kit/ibr` warmup behavior

```yaml
warmup:
  tool: kit/ibr
  cadence: weekly
  run_window: "00:00-04:00"
  inputs:
    - jurisdiction
    - registry
    - search_url
    - query_example.name
  checks:
    - page_loads
    - search_input_detected
    - search_submit_detected
    - result_list_detected
    - detail_page_detected
    - expected_fields_detected
  on_breakage:
    - mark_recipe_broken
    - keep_last_known_good_recipe
    - open_approval_suggestion
    - propose_updated_selectors_with_kit_llm
    - evaluate_updated_recipe_with_kit_eva
```

## Output shape

```json
{
  "jurisdiction": "US-DE",
  "registry": "Delaware Division of Corporations",
  "recipe_id": "business-registry-search/us-de",
  "status": "ok",
  "last_checked": "2026-05-08T02:15:00-04:00",
  "tooling": {
    "scrape": "kit/ibr",
    "evaluate": "kit/eva",
    "ai": "kit.llm"
  },
  "fields_detected": ["legal_name", "registry_id", "status", "registered_address"],
  "confidence": 0.91
}
```
