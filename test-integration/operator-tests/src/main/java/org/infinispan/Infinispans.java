package org.infinispan;

public class Infinispans {

   public static Infinispan defaultAuth() {
      return new Infinispan("src/test/resources/infinispans/cr_minimal.yaml");
   }

   public static Infinispan customAuth() {
      return new Infinispan("src/test/resources/infinispans/cr_minimal_with_auth.yaml");
   }

   public static Infinispan encryptionExternal() {
      return new Infinispan("src/test/resources/infinispans/cr_encryption_external.yaml");
   }

   public static Infinispan encryptionProvided() {
      return new Infinispan("src/test/resources/infinispans/cr_encryption_provided.yaml");
   }
}
