# Handoff

WP-07 must dispatch only from server-verified Claims and the immutable run target, with current admission/permissions checked before each call and retry. A bearer credential is not route admission. Never route through Group.Picked or a client account/role header. Worker environments must never receive management credentials.
