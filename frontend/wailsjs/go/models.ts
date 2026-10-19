export namespace core {
	
	export class AgentContinuation {
	    runID: string;
	    instruction: string;
	    productText: string;
	    references: string[];
	    answers: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new AgentContinuation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.runID = source["runID"];
	        this.instruction = source["instruction"];
	        this.productText = source["productText"];
	        this.references = source["references"];
	        this.answers = source["answers"];
	    }
	}
	export class AgentEvent {
	    at: string;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.at = source["at"];
	        this.text = source["text"];
	    }
	}
	export class AgentInput {
	    productURL: string;
	    instruction: string;
	    productText: string;
	    references: string[];
	    projectID: string;
	    targetCount: number;
	    ratio: string;
	    size: string;
	    quality: string;
	    llmConnectionID: string;
	    imageConnectionID: string;
	    imageModel: string;
	    skillIDs: string[];
	
	    static createFrom(source: any = {}) {
	        return new AgentInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.productURL = source["productURL"];
	        this.instruction = source["instruction"];
	        this.productText = source["productText"];
	        this.references = source["references"];
	        this.projectID = source["projectID"];
	        this.targetCount = source["targetCount"];
	        this.ratio = source["ratio"];
	        this.size = source["size"];
	        this.quality = source["quality"];
	        this.llmConnectionID = source["llmConnectionID"];
	        this.imageConnectionID = source["imageConnectionID"];
	        this.imageModel = source["imageModel"];
	        this.skillIDs = source["skillIDs"];
	    }
	}
	export class SuiteItem {
	    id: string;
	    purpose: string;
	    scene: string;
	    prompt: string;
	    references: string[];
	    size: string;
	    quality: string;
	
	    static createFrom(source: any = {}) {
	        return new SuiteItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.purpose = source["purpose"];
	        this.scene = source["scene"];
	        this.prompt = source["prompt"];
	        this.references = source["references"];
	        this.size = source["size"];
	        this.quality = source["quality"];
	    }
	}
	export class SuitePlan {
	    revision: number;
	    createdAt: string;
	    locked: string[];
	    editable: string[];
	    notes: string;
	    items: SuiteItem[];
	
	    static createFrom(source: any = {}) {
	        return new SuitePlan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.revision = source["revision"];
	        this.createdAt = source["createdAt"];
	        this.locked = source["locked"];
	        this.editable = source["editable"];
	        this.notes = source["notes"];
	        this.items = this.convertValues(source["items"], SuiteItem);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AgentQuestion {
	    id: string;
	    label: string;
	    required: boolean;
	    answer: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentQuestion(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.required = source["required"];
	        this.answer = source["answer"];
	    }
	}
	export class ProductInfo {
	    imageSources: Record<string, string>;
	    sourceURL: string;
	    title: string;
	    description: string;
	    facts: string;
	    error: string;
	    referenceIDs: string[];
	
	    static createFrom(source: any = {}) {
	        return new ProductInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.imageSources = source["imageSources"];
	        this.sourceURL = source["sourceURL"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.facts = source["facts"];
	        this.error = source["error"];
	        this.referenceIDs = source["referenceIDs"];
	    }
	}
	export class SkillAttachment {
	    name: string;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new SkillAttachment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	    }
	}
	export class SkillRecord {
	    id: string;
	    name: string;
	    description: string;
	    content: string;
	    version: number;
	    enabled: boolean;
	    source: string;
	    attachments: SkillAttachment[];
	
	    static createFrom(source: any = {}) {
	        return new SkillRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.content = source["content"];
	        this.version = source["version"];
	        this.enabled = source["enabled"];
	        this.source = source["source"];
	        this.attachments = this.convertValues(source["attachments"], SkillAttachment);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LLMCapabilities {
	    model: string;
	    text: boolean;
	    vision: boolean;
	    tools: boolean;
	    streaming: boolean;
	    checkedAt: string;
	    details: string[];
	
	    static createFrom(source: any = {}) {
	        return new LLMCapabilities(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.model = source["model"];
	        this.text = source["text"];
	        this.vision = source["vision"];
	        this.tools = source["tools"];
	        this.streaming = source["streaming"];
	        this.checkedAt = source["checkedAt"];
	        this.details = source["details"];
	    }
	}
	export class Connection {
	    maxOutputTokens?: number;
	    kind: string;
	    mode?: string;
	    authMode?: string;
	    capabilities?: LLMCapabilities;
	    id: string;
	    name: string;
	    baseURL: string;
	    model: string;
	    protocol: string;
	    credential?: string;
	    hasKey: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Connection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.maxOutputTokens = source["maxOutputTokens"];
	        this.kind = source["kind"];
	        this.mode = source["mode"];
	        this.authMode = source["authMode"];
	        this.capabilities = this.convertValues(source["capabilities"], LLMCapabilities);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.baseURL = source["baseURL"];
	        this.model = source["model"];
	        this.protocol = source["protocol"];
	        this.credential = source["credential"];
	        this.hasKey = source["hasKey"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AgentRun {
	    autoGeneration: boolean;
	    authorizedCount: number;
	    imageConnection: Connection;
	    submissionID: string;
	    attemptStartRevision: number;
	    analysisSucceeded: boolean;
	    cancelRequested: boolean;
	    analysisStartedAt: string;
	    instructions: string[];
	    id: string;
	    createdAt: string;
	    updatedAt: string;
	    status: string;
	    stage: string;
	    error: string;
	    input: AgentInput;
	    llmConnection: Connection;
	    skills: SkillRecord[];
	    product: ProductInfo;
	    questions: AgentQuestion[];
	    plans: SuitePlan[];
	    events: AgentEvent[];
	    batchID: string;
	    submittedRevision: number;
	
	    static createFrom(source: any = {}) {
	        return new AgentRun(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.autoGeneration = source["autoGeneration"];
	        this.authorizedCount = source["authorizedCount"];
	        this.imageConnection = this.convertValues(source["imageConnection"], Connection);
	        this.submissionID = source["submissionID"];
	        this.attemptStartRevision = source["attemptStartRevision"];
	        this.analysisSucceeded = source["analysisSucceeded"];
	        this.cancelRequested = source["cancelRequested"];
	        this.analysisStartedAt = source["analysisStartedAt"];
	        this.instructions = source["instructions"];
	        this.id = source["id"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.status = source["status"];
	        this.stage = source["stage"];
	        this.error = source["error"];
	        this.input = this.convertValues(source["input"], AgentInput);
	        this.llmConnection = this.convertValues(source["llmConnection"], Connection);
	        this.skills = this.convertValues(source["skills"], SkillRecord);
	        this.product = this.convertValues(source["product"], ProductInfo);
	        this.questions = this.convertValues(source["questions"], AgentQuestion);
	        this.plans = this.convertValues(source["plans"], SuitePlan);
	        this.events = this.convertValues(source["events"], AgentEvent);
	        this.batchID = source["batchID"];
	        this.submittedRevision = source["submittedRevision"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AgentPage {
	    items: AgentRun[];
	    total: number;
	
	    static createFrom(source: any = {}) {
	        return new AgentPage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], AgentRun);
	        this.total = source["total"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class AgentRuntimeInfo {
	    available: boolean;
	    version: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentRuntimeInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.version = source["version"];
	        this.message = source["message"];
	    }
	}
	export class Asset {
	    id: string;
	    name: string;
	    path: string;
	    thumbnail: string;
	    kind: string;
	    taskID: string;
	    projectID: string;
	    createdAt: string;
	    width: number;
	    height: number;
	    hidden: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Asset(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.thumbnail = source["thumbnail"];
	        this.kind = source["kind"];
	        this.taskID = source["taskID"];
	        this.projectID = source["projectID"];
	        this.createdAt = source["createdAt"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.hidden = source["hidden"];
	    }
	}
	export class AssetPage {
	    items: Asset[];
	    total: number;
	
	    static createFrom(source: any = {}) {
	        return new AssetPage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], Asset);
	        this.total = source["total"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class ConnectionInput {
	    maxOutputTokens: number;
	    kind: string;
	    mode: string;
	    authMode: string;
	    id: string;
	    name: string;
	    baseURL: string;
	    model: string;
	    key: string;
	    clearKey: boolean;
	    testRequestID?: string;
	
	    static createFrom(source: any = {}) {
	        return new ConnectionInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.maxOutputTokens = source["maxOutputTokens"];
	        this.kind = source["kind"];
	        this.mode = source["mode"];
	        this.authMode = source["authMode"];
	        this.id = source["id"];
	        this.name = source["name"];
	        this.baseURL = source["baseURL"];
	        this.model = source["model"];
	        this.key = source["key"];
	        this.clearKey = source["clearKey"];
	        this.testRequestID = source["testRequestID"];
	    }
	}
	export class GenerateInput {
	    requestID: string;
	    connectionID: string;
	    model: string;
	    prompts: string[];
	    count: number;
	    references: string[];
	    size: string;
	    quality: string;
	    projectID: string;
	
	    static createFrom(source: any = {}) {
	        return new GenerateInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requestID = source["requestID"];
	        this.connectionID = source["connectionID"];
	        this.model = source["model"];
	        this.prompts = source["prompts"];
	        this.count = source["count"];
	        this.references = source["references"];
	        this.size = source["size"];
	        this.quality = source["quality"];
	        this.projectID = source["projectID"];
	    }
	}
	export class GenerationDiagnostic {
	    stage: string;
	    startedAt: string;
	    requestURL: string;
	    httpStatus: number;
	    contentType: string;
	    contentLength: number;
	    transferEncoding: string;
	    httpProtocol: string;
	    networkRoute: string;
	    upstreamRequestID: string;
	    elapsedMS: number;
	    bytesReceived: number;
	    networkError: string;
	    downloadStartedAt?: string;
	    downloadMS?: number;
	
	    static createFrom(source: any = {}) {
	        return new GenerationDiagnostic(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.stage = source["stage"];
	        this.startedAt = source["startedAt"];
	        this.requestURL = source["requestURL"];
	        this.httpStatus = source["httpStatus"];
	        this.contentType = source["contentType"];
	        this.contentLength = source["contentLength"];
	        this.transferEncoding = source["transferEncoding"];
	        this.httpProtocol = source["httpProtocol"];
	        this.networkRoute = source["networkRoute"];
	        this.upstreamRequestID = source["upstreamRequestID"];
	        this.elapsedMS = source["elapsedMS"];
	        this.bytesReceived = source["bytesReceived"];
	        this.networkError = source["networkError"];
	        this.downloadStartedAt = source["downloadStartedAt"];
	        this.downloadMS = source["downloadMS"];
	    }
	}
	
	export class LogEntry {
	    id: number;
	    time: string;
	    level: string;
	    source: string;
	    relatedID: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new LogEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.time = source["time"];
	        this.level = source["level"];
	        this.source = source["source"];
	        this.relatedID = source["relatedID"];
	        this.message = source["message"];
	    }
	}
	export class LogPage {
	    items: LogEntry[];
	    total: number;
	
	    static createFrom(source: any = {}) {
	        return new LogPage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], LogEntry);
	        this.total = source["total"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Output {
	    url?: string;
	    b64_json?: string;
	
	    static createFrom(source: any = {}) {
	        return new Output(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.b64_json = source["b64_json"];
	    }
	}
	export class Task {
	    agentRunID?: string;
	    planItemID?: string;
	    refinedFrom?: string;
	    refineInstruction?: string;
	    previousAssetIDs?: string[];
	    id: string;
	    batchID: string;
	    createdAt: string;
	    updatedAt: string;
	    status: string;
	    error: string;
	    prompt: string;
	    model: string;
	    size: string;
	    quality: string;
	    references: string[];
	    projectID: string;
	    connection: Connection;
	    results?: Output[];
	    assetIDs: string[];
	    diagnostic?: GenerationDiagnostic;
	
	    static createFrom(source: any = {}) {
	        return new Task(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.agentRunID = source["agentRunID"];
	        this.planItemID = source["planItemID"];
	        this.refinedFrom = source["refinedFrom"];
	        this.refineInstruction = source["refineInstruction"];
	        this.previousAssetIDs = source["previousAssetIDs"];
	        this.id = source["id"];
	        this.batchID = source["batchID"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.status = source["status"];
	        this.error = source["error"];
	        this.prompt = source["prompt"];
	        this.model = source["model"];
	        this.size = source["size"];
	        this.quality = source["quality"];
	        this.references = source["references"];
	        this.projectID = source["projectID"];
	        this.connection = this.convertValues(source["connection"], Connection);
	        this.results = this.convertValues(source["results"], Output);
	        this.assetIDs = source["assetIDs"];
	        this.diagnostic = this.convertValues(source["diagnostic"], GenerationDiagnostic);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Page {
	    items: Task[];
	    total: number;
	
	    static createFrom(source: any = {}) {
	        return new Page(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], Task);
	        this.total = source["total"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class Project {
	    id: string;
	    name: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new Project(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class RecentBatch {
	    id: string;
	    createdAt: string;
	    total: number;
	    completed: number;
	    active: number;
	    imageCount: number;
	    tasks: Task[];
	
	    static createFrom(source: any = {}) {
	        return new RecentBatch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.createdAt = source["createdAt"];
	        this.total = source["total"];
	        this.completed = source["completed"];
	        this.active = source["active"];
	        this.imageCount = source["imageCount"];
	        this.tasks = this.convertValues(source["tasks"], Task);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RefineInput {
	    requestID: string;
	    taskID: string;
	    assetID: string;
	    instruction: string;
	
	    static createFrom(source: any = {}) {
	        return new RefineInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requestID = source["requestID"];
	        this.taskID = source["taskID"];
	        this.assetID = source["assetID"];
	        this.instruction = source["instruction"];
	    }
	}
	
	
	export class SmartProgress {
	    runID: string;
	    state: string;
	    done: number;
	    total: number;
	
	    static createFrom(source: any = {}) {
	        return new SmartProgress(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.runID = source["runID"];
	        this.state = source["state"];
	        this.done = source["done"];
	        this.total = source["total"];
	    }
	}
	
	
	export class Summary {
	    tasks: number;
	    assets: number;
	    active: number;
	    dataDir: string;
	    version: string;
	
	    static createFrom(source: any = {}) {
	        return new Summary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tasks = source["tasks"];
	        this.assets = source["assets"];
	        this.active = source["active"];
	        this.dataDir = source["dataDir"];
	        this.version = source["version"];
	    }
	}
	
	export class Template {
	    id: string;
	    name: string;
	    prompt: string;
	    size: string;
	    quality: string;
	
	    static createFrom(source: any = {}) {
	        return new Template(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.prompt = source["prompt"];
	        this.size = source["size"];
	        this.quality = source["quality"];
	    }
	}
	export class TestResult {
	    auth: string;
	    models: string;
	    generation: string;
	    ids: string[];
	
	    static createFrom(source: any = {}) {
	        return new TestResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.auth = source["auth"];
	        this.models = source["models"];
	        this.generation = source["generation"];
	        this.ids = source["ids"];
	    }
	}

}

