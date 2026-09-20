# 0007i - Single real-time Portal cutover

Complete the frontend replacement so the Phoenix release serves one coherent
workspace for every supported workflow. Real-time invalidation, reconnect
snapshot refresh and cursor catch-up, unified navigation, command discovery,
legacy hash redirects, and removal of duplicate legacy presentation leave one
writable UI state owner.

Completion evidence includes the full frontend, contract, browser, accessibility,
and Phoenix release gates on a clean revision; end-to-end flows for expired auth,
reconnect, deep links, focus restoration, reduced motion, and narrow screens; and
owner visual acceptance across all real workflows delivered by 0007a-0007h.

This goal depends on 0007a-0007h and introduces no production Node server,
production mock data, workflow canvas, or mobile application. It is one final
independently mergeable PR and must fit one agent run of at most 12 hours.
