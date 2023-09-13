import build from "pino-abstract-transport";
import SonicBoom from "sonic-boom";

// Map of module names to SonicBoom instances
const outputs: Record<string, SonicBoom> = {};

export default async function (opts: any) {
    return build(function (source: any) {
        source.on("data", function (obj: any) {
            const { module } = obj;
            if (!outputs[module]) {
                // Create a new SonicBoom for this module if it doesn't exist yet
                outputs[module] = new SonicBoom({
                    dest: `./${module}-logs.txt`,
                    sync: false,
                });
            }
            // Write the log record to the appropriate file
            const log = JSON.stringify(obj);
            outputs[module].write(`${log}\n`);
        });
    });
}
