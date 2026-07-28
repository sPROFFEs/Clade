export * as Observability from "./observability"

import { LayerNode } from "./effect/layer-node"
import { Layer, Logger, References } from "effect"

const discardLogger = Logger.make(() => {})

export const layer = Logger.layer([discardLogger], { mergeWithExisting: false }).pipe(
  Layer.merge(Layer.succeed(References.MinimumLogLevel, "Info")),
)

export const node = LayerNode.make({ name: "observability", layer, deps: [] })
