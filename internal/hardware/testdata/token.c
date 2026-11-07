/* Synthetic PKCS#11 ABI fixture. Contains no real token or private key. */
#include <stdint.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>
typedef unsigned long W;
typedef struct { W type; void *value; W size; } Attribute;
typedef struct { unsigned char major,minor; void *fn[68]; } Functions;
static W selected_class,logins,slot_calls;
static int emitted;
static W initialize(void *ignored){logins=0;slot_calls=0;return 0;}
static W finalize(void *ignored){return 0;}
static W slots(unsigned char present,W *out,W *count){int growing=getenv("SMALLSSH_TEST_GROWING_SLOTS")!=NULL;W n=(getenv("SMALLSSH_TEST_DUPLICATE_SERIAL")||getenv("SMALLSSH_TEST_REMOVED_SLOT")||(growing&&slot_calls>0))?2:1;slot_calls++;if(out&&*count<n){*count=n;return 0x150;}if(out){out[0]=7;if(n==2)out[1]=8;}*count=n;return 0;}
static W token(W slot,unsigned char *out){if(slot==7&&getenv("SMALLSSH_TEST_REMOVED_SLOT"))return 0xe0;memset(out,' ',160);memcpy(out,"Synthetic token",15);memcpy(out+64,"Fixture RSA/EC",14);memcpy(out+80,slot==8&&getenv("SMALLSSH_TEST_GROWING_SLOTS")?"fixture0002":"fixture0001",11);memset(out+96,0,64);return 0;}
static W open_session(W slot,W flags,void *app,void *notify,W *session){*session=9;return 0;}
static W close_session(W session){return 0;}
static W login(W session,W user,unsigned char *pin,W length){logins++;return length==7&&!memcmp(pin,"testpin",7)?0:0xa0;}
static W find_init(W session,Attribute *attrs,W count){selected_class=99;for(W i=0;i<count;i++)if(attrs[i].type==0)memcpy(&selected_class,attrs[i].value,sizeof(W));emitted=0;return 0;}
static W find_objects(W session,W *objects,W maximum,W *count){*count=0;if(!emitted&&(selected_class==3||(selected_class==1&&!getenv("SMALLSSH_TEST_PUBLIC_ONLY"))||(selected_class==2&&getenv("SMALLSSH_TEST_PUBLIC_ONLY")))){objects[0]=selected_class==1?10:selected_class==2?12:11;*count=1;emitted=1;}return 0;}
static W find_final(W session){return 0;}
static W get_attribute(W session,W object,Attribute *attrs,W count){
 unsigned char cert[16384],id=1;W ec=3;size_t size=0;
 for(W i=0;i<count;i++){
  void *source=NULL;size_t length=0;
  if(attrs[i].type==0x102){source=&id;length=1;}
  else if(attrs[i].type==3){source="fixture certificate";length=19;}
  else if(object==12&&attrs[i].type==0x100){source=&ec;length=sizeof(ec);}
  else if(object==12&&(attrs[i].type==0x180||attrs[i].type==0x181)){const char *path=getenv(attrs[i].type==0x180?"SMALLSSH_TEST_EC_PARAMS":"SMALLSSH_TEST_EC_POINT");if(!path)return 5;FILE *file=fopen(path,"rb");if(!file)return 5;size=fread(cert,1,sizeof(cert),file);fclose(file);source=cert;length=size;}
  else if(attrs[i].type==0x11){const char *path=getenv("SMALLSSH_TEST_TOKEN_CERT");if(!path)return 5;FILE *file=fopen(path,"rb");if(!file)return 5;size=fread(cert,1,sizeof(cert),file);fclose(file);source=cert;length=size;}
  else return 0x12;
  if(attrs[i].value){if(attrs[i].size<length)return 0x150;memcpy(attrs[i].value,source,length);}attrs[i].size=(W)length;
 }
 return 0;
}
static W sign_init(W session,void *mechanism,W key){return 0;}
static W sign_value(W session,void *input,W length,unsigned char *signature,W *size){
 const char *marker=getenv("SMALLSSH_TEST_TOKEN_BLOCK");
 if(marker){FILE *f=fopen(marker,"wb");if(f){fputc('1',f);fclose(f);}for(;;)sleep(1);}
 if(*size<64)return 0x150;memset(signature,0,64);signature[31]=1;signature[63]=1;*size=64;return 0;
}
W FixtureLoginCount(void){return logins;}
W C_GetFunctionList(Functions **out){
 if(getenv("SMALLSSH_TEST_FUNCTION_LIST_ERROR"))return 5;
 static Functions f;f.major=2;f.minor=40;
 f.fn[0]=(void*)initialize;f.fn[1]=(void*)finalize;f.fn[4]=(void*)slots;f.fn[6]=(void*)token;
 f.fn[12]=(void*)open_session;f.fn[13]=(void*)close_session;f.fn[18]=(void*)login;
 f.fn[24]=(void*)get_attribute;f.fn[26]=(void*)find_init;f.fn[27]=(void*)find_objects;f.fn[28]=(void*)find_final;
 f.fn[42]=(void*)sign_init;f.fn[43]=(void*)sign_value;*out=&f;return 0;
}
