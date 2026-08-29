import { getDemoSnapshot } from "@/features/demo/demo-adapter";
import { DemoWorkspace } from "@/features/demo/demo-workspace";

const DemoPage = () => <DemoWorkspace snapshot={getDemoSnapshot()} />;

export default DemoPage;
