# Synchronized workspace bot classification

The background DM sweep writes classifications while the serialized event
owner constructs conversations. A plain shared map can crash or race even without
starred hydration. Encapsulate positive classifications in a private, zero-value
ready `sync.Map`; access only through `MarkBotUser` and `IsBotUser`.

Startup/cache/boot/sweep consumers use the same accessors. Failed or unknown
lookups cannot erase an established positive classification. Existing fixture
changes are mechanical. Tests cover nil/empty/zero-value behavior and the actual
DM sweep running concurrently with ordinary conversation construction under race
detection. No starred membership or snapshot repair is introduced here.
