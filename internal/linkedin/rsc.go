package linkedin

// rsc.go — the RSC (React Flight) pipeline is RETIRED except for the
// contact-info overlay (contact.go): the voyager dash + recommendations
// endpoints replaced every section call. What remains here are the two
// LinkedIn web-build tracking constants the overlay call still mirrors.

// liAppVersion / liTrack mirror LinkedIn's current web build (recon round 4,
// Aug 2026: 0.2.6975). LinkedIn tolerates stale versions, but keep them
// current — they live in ONE place so a re-capture means a one-line change.
const liAppVersion = "0.2.6975"
const liTrack = `{"clientVersion":"` + liAppVersion + `","mpVersion":"` + liAppVersion + `","osName":"web","timezoneOffset":5.5,"timezone":"Asia/Calcutta","deviceFormFactor":"DESKTOP","mpName":"web","displayDensity":1,"displayWidth":1280,"displayHeight":720}`
