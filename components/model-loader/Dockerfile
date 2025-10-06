FROM alpine:3.22

# Common
# * unzip (full version) needed for installing ossutil
RUN apk add --no-cache curl unzip tar

# Python
ENV PYTHONUNBUFFERED=1
RUN apk add --no-cache python3 py3-pip pipx bash && rm -rf ~/.cache/* /usr/local/share/man /tmp/*     
# Location where pipx installs executables:
ENV PATH="/root/.local/bin:$PATH"

# Hugging Face ("hf://")
RUN pipx install huggingface_hub
RUN huggingface-cli version

# AWS S3 ("s3://")
RUN pipx install awscli
RUN aws --version

# Determine architecture.
RUN if [ `uname -m` = 'x86_64' ]; then echo -n "x86_64" > /tmp/arch; else echo -n "arm" > /tmp/arch; fi;

# Google Cloud Storage ("gs://")
RUN ARCH=`cat /tmp/arch` && curl -OL https://dl.google.com/dl/cloudsdk/channels/rapid/downloads/google-cloud-cli-linux-$ARCH.tar.gz
RUN ls
RUN ARCH=`cat /tmp/arch` && tar -xf google-cloud-cli-linux-$ARCH.tar.gz
RUN ./google-cloud-sdk/install.sh --usage-reporting=false
ENV PATH="/google-cloud-sdk/bin:$PATH"
RUN gcloud config set component_manager/disable_update_check true
RUN gcloud --version

# Alibaba Object Storage Service ("oss://")
RUN wget -O - https://gosspublic.alicdn.com/ossutil/install.sh | bash
RUN ossutil --version

# Loader script
COPY ./load.sh /bin/load
RUN chmod +x /bin/load
ENTRYPOINT ["/bin/load"]