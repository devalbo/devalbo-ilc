import { createNodeEnvironment } from "../src/hosts/node.js";
import { hello } from "../src/hello.js";

await hello(createNodeEnvironment());
