/// <reference lib="deno.ns" />
/// <reference lib="dom" />
import { assertEquals, assertRejects } from "jsr:@std/assert@1";

Deno.test("failed logout preserves the user and allows successful retry", async () => {
  const oldWindow=Object.getOwnPropertyDescriptor(globalThis,"window");
  const oldDocument=Object.getOwnPropertyDescriptor(globalThis,"document");
  const originalFetch=globalThis.fetch;
  Object.defineProperty(globalThis,"window",{configurable:true,value:{matchMedia:()=>({matches:false})}});
  Object.defineProperty(globalThis,"document",{configurable:true,value:{documentElement:{dataset:{}}}});
  try {
    const state=await import("./state.ts");
    const user={id:"alice",name:"Alice",email:"alice@example.com",created_at:"2026-11-11T00:00:00Z"};
    state.signIn(user);
    globalThis.fetch=()=>Promise.reject(new TypeError("network unavailable"));
    await assertRejects(()=>state.signOut());
    assertEquals(state.phase(),"authenticated");assertEquals(state.currentUser(),user);
    globalThis.fetch=()=>Promise.resolve(new Response(JSON.stringify({error:{message:"revocation failed"}}),{status:500}));
    await assertRejects(()=>state.signOut());
    assertEquals(state.phase(),"authenticated");assertEquals(state.currentUser(),user);
    globalThis.fetch=()=>Promise.resolve(new Response(null,{status:204}));
    await state.signOut();assertEquals(state.phase(),"anonymous");assertEquals(state.currentUser(),null);
  } finally {
    globalThis.fetch=originalFetch;
    if(oldWindow)Object.defineProperty(globalThis,"window",oldWindow);else Reflect.deleteProperty(globalThis,"window");
    if(oldDocument)Object.defineProperty(globalThis,"document",oldDocument);else Reflect.deleteProperty(globalThis,"document");
  }
});

Deno.test("onboarding dismissal parsing tolerates broken or blocked storage and isolates accounts", async()=>{
 const old=Object.getOwnPropertyDescriptor(globalThis,"localStorage");
 const memory=new Map<string,string>();
 Object.defineProperty(globalThis,"localStorage",{configurable:true,value:{getItem:(key:string)=>memory.get(key)??null,setItem:(key:string,value:string)=>memory.set(key,value)}});
 try {
 const state=await import("./state.ts");
 state.saveOnboardingDismissals("alice",["https","backup"]);
 assertEquals(state.readOnboardingDismissals("alice"),["https","backup"]);
 assertEquals(state.readOnboardingDismissals("bob"),[]);
 memory.set("pier.onboarding.alice","invalid JSON");assertEquals(state.readOnboardingDismissals("alice"),[]);
 memory.set("pier.onboarding.alice",'{"backup":true}');assertEquals(state.readOnboardingDismissals("alice"),[]);
 Object.defineProperty(globalThis,"localStorage",{configurable:true,get(){throw new Error("blocked");}});
 assertEquals(state.readOnboardingDismissals("alice"),[]);state.saveOnboardingDismissals("alice",["backup"]);
 } finally {if(old)Object.defineProperty(globalThis,"localStorage",old);else Reflect.deleteProperty(globalThis,"localStorage");}
});

Deno.test("the getting started card remembers per user whether it is collapsed or closed", async () => {
  const oldWindow=Object.getOwnPropertyDescriptor(globalThis,"window");
  const oldDocument=Object.getOwnPropertyDescriptor(globalThis,"document");
  const oldStorage=Object.getOwnPropertyDescriptor(globalThis,"localStorage");
  const store=new Map<string,string>();
  Object.defineProperty(globalThis,"window",{configurable:true,value:{matchMedia:()=>({matches:false})}});
  Object.defineProperty(globalThis,"document",{configurable:true,value:{documentElement:{dataset:{}}}});
  Object.defineProperty(globalThis,"localStorage",{configurable:true,value:{getItem:(k:string)=>store.get(k)??null,setItem:(k:string,v:string)=>void store.set(k,v)}});
  try {
    const state=await import("./state.ts");
    assertEquals(state.readOnboardingView("ana"),"open");
    state.saveOnboardingView("ana","collapsed");
    state.saveOnboardingView("bia","closed");
    assertEquals([state.readOnboardingView("ana"),state.readOnboardingView("bia")],["collapsed","closed"]);
    store.set("pier.onboarding.view.ana","tampered");
    assertEquals(state.readOnboardingView("ana"),"open");
  } finally {
    for (const [name,descriptor] of [["window",oldWindow],["document",oldDocument],["localStorage",oldStorage]] as const) {
      if (descriptor) Object.defineProperty(globalThis,name,descriptor); else delete (globalThis as Record<string,unknown>)[name];
    }
  }
});
