import { apiError } from "~/utils/apiError";

const authHeader = (): Record<string, string> => {
  if (!import.meta.client) return {};
  const token = localStorage.getItem("auth_token");
  return token ? { Authorization: `Bearer ${token}` } : {};
};

export interface CfoRegistrySheet { code:number; name:string; label:string; fields:{key:string;label:string;type:string}[]; configuration_pending?:boolean; shared_book?:boolean }
export interface CfoRegistryRow { id:number; sheet:string; attrs:Record<string,unknown>; months:Record<string,number|null>; total?:number; issues?:{code:string;message:string;blocking:boolean}[] }
export interface CfoRegistry { card_id:number; code:number; country:string; book_scope:string; sheets:CfoRegistrySheet[]; sheet:string; rows:CfoRegistryRow[]; read_only:boolean; total:number; configuration_pending?:boolean }
export function useCfoExpenseRegistry(){
 const loading=ref(false);const error=ref("");
 const base=useRuntimeConfig().public.apiBase;
 type RequestOptions={method?:"GET"|"POST"|"PUT"|"DELETE";body?:unknown};
 async function request<T>(url:string,opts:RequestOptions={}):Promise<T>{loading.value=true;error.value="";try{return await $fetch<T>(url,{...opts,headers:authHeader()} as any) as unknown as T}catch(e){error.value=apiError(e);throw e}finally{loading.value=false}}
 const path=(cardId:number,sheet:string)=>`${base}/api/plans/cfo-exp/${cardId}/registry/${encodeURIComponent(sheet)}`;
 const get=(cardId:number,sheet:string)=>request<CfoRegistry>(path(cardId,sheet))
 const save=(cardId:number,sheet:string,row:Partial<CfoRegistryRow>)=>request<{id:number}>(path(cardId,sheet),{method:"PUT",body:row})
 const copy=(cardId:number,sheet:string,id:number)=>request<{id:number}>(`${path(cardId,sheet)}/${id}/copy`,{method:"POST"})
 const remove=(cardId:number,sheet:string,id:number)=>request<{ok:boolean}>(`${path(cardId,sheet)}/${id}`,{method:"DELETE"})
 return {loading,error,get,save,copy,remove}
}
